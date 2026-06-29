// 模型三「事件驱动（gnet / reactor）」的完整可运行 demo。
//
// 对应文章：Go 长连接服务的三种读写并发模型 · §2.3
// https://wenfh2020.com/2026/06/25/go-longconn-concurrency-models/
//
// 程序自带 server + client，同进程自驱动，无需任何外部服务即可运行：
//
//	go run .          普通运行，观察日志
//	go run -race .    开启竞态检测——核心看点：同一连接被「event-loop 回调(回 pong)」
//	                  和「多个业务推送协程」并发写，全靠 gnet 的 AsyncWrite 把数据丢进
//	                  框架内部写队列、由 event-loop 串行落地，所以业务侧无需任何锁，
//	                  -race 下干净无告警。
//
// 与模型一/二的根本差别：没有「每连接一个读协程」。所有 fd 挂在 epoll 上，由少量
// event-loop 协程（≈ CPU 核数）统一管理；内核说哪个 fd 可读，就回调谁的 OnTraffic。
// demo 会连上 200 条连接，并打印 goroutine 数量——你会看到服务端 event-loop 协程数
// 固定为「核数」，和连接数完全解耦（这正是模型三能扛百万长连接的根本原因）。
//
// 注意：gnet 工作在裸 TCP 之上，没有 websocket 的「消息」边界，所以要自己粘包拆包。
// 本 demo 用最简单的「4 字节大端长度前缀 + 包体」协议：[len:4][payload:len]。
package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/panjf2000/gnet/v2"
)

// ───────────────────────── 协议：4 字节长度前缀粘包拆包 ─────────────────────────

const headerLen = 4 // 包头：4 字节大端，表示后面包体的字节数

// encode 把一段 payload 封成 [len:4][payload] 的整包。客户端、服务端回包都用它。
func encode(payload []byte) []byte {
	buf := make([]byte, headerLen+len(payload))
	binary.BigEndian.PutUint32(buf, uint32(len(payload)))
	copy(buf[headerLen:], payload)
	return buf
}

// readFullPacket 从 gnet 连接的入站缓冲里「凑齐一个整包」就取出来，凑不齐就返回 nil。
//
// 这是 reactor 模型最啰嗦、也最容易出错的地方——TCP 是字节流，一次 OnTraffic 里可能
// 只到了半个包，也可能堆了好几个包。所以必须：
//   - 不够一个包头(4B) → 半个包，返回 nil，等下次 OnTraffic；
//   - 包头有了但包体没凑齐 → 半个包，返回 nil，等下次 OnTraffic；
//   - 凑齐了 → Discard 掉包头、Next 取出包体。
//
// 关键坑：c.Next/c.Peek 返回的切片指向 gnet 内部缓冲，OnTraffic 回调返回后会被复用。
// 想把它留到回调之外（比如丢给新协程、塞进 map），必须先 copy 一份，否则数据会被串改。
func readFullPacket(c gnet.Conn) ([]byte, error) {
	if c.InboundBuffered() < headerLen {
		return nil, nil // 连包头都没凑齐
	}
	hdr, _ := c.Peek(headerLen) // Peek 只看不消费
	bodyLen := int(binary.BigEndian.Uint32(hdr))
	if c.InboundBuffered() < headerLen+bodyLen {
		return nil, nil // 包头到了，包体还没齐
	}
	if _, err := c.Discard(headerLen); err != nil { // 丢弃包头
		return nil, err
	}
	body, err := c.Next(bodyLen) // 取出包体（指向内部缓冲）
	if err != nil {
		return nil, err
	}
	// body 指向 gnet 内部缓冲，回调返回即被复用 → copy 一份再交出去。
	out := make([]byte, bodyLen)
	copy(out, body)
	return out, nil
}

// ───────────────────────── 每连接上下文，挂在 gnet.Conn 上 ─────────────────────────

// ConnContext 每条连接的状态。挂在 gnet.Conn 自己的 context 上（SetContext/Context）。
// 因为同一连接的所有回调(OnOpen/OnTraffic/OnClose)都跑在同一个 event-loop 协程里、
// 彼此严格串行，所以读写这个 context 天然不需要加锁。
type ConnContext struct {
	ConnId    string
	Addr      string
	StartTime time.Time
}

var connSeq uint64 // demo 用自增 id 代替 UUID，免引第三方依赖

func genConnId() string {
	return fmt.Sprintf("conn-%d", atomic.AddUint64(&connSeq, 1))
}

// ───────────────────────── 连接管理器：connId -> gnet.Conn ─────────────────────────

// ClientManager 连接表，让服务端能按 id 主动找到某条连接去推送。
// 这里被「event-loop 协程(OnOpen/OnClose)」和「业务推送协程」并发读写，所以要加锁——
// 注意：要加锁的是这张全局表，不是单条连接的写；单条连接的写并发安全由 gnet 的
// AsyncWrite 自己保证。
type ClientManager struct {
	mu    sync.RWMutex
	conns map[string]gnet.Conn
}

func NewClientManager() *ClientManager {
	return &ClientManager{conns: make(map[string]gnet.Conn)}
}

func (m *ClientManager) Add(id string, c gnet.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.conns[id] = c
}

func (m *ClientManager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.conns, id)
}

func (m *ClientManager) Get(id string) (gnet.Conn, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.conns[id]
	return c, ok
}

func (m *ClientManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.conns)
}

// Push 按 id 找到连接，异步推一条消息。AsyncWrite 把数据丢进 gnet 写队列，
// 框架保证同一连接的并发写安全，业务侧无需加锁。
func (m *ClientManager) Push(id string, payload []byte) bool {
	c, ok := m.Get(id)
	if !ok {
		return false
	}
	return c.AsyncWrite(encode(payload), nil) == nil
}

// ───────────────────────────────── server（gnet 事件回调）─────────────────────────

var clientMgr = NewClientManager()

// Server 实现 gnet 的事件回调。内嵌 BuiltinEventEngine 拿到所有回调的默认空实现，
// 只覆盖关心的几个。整个进程只有这一个 Server 实例，被所有 event-loop 协程共享。
type Server struct {
	gnet.BuiltinEventEngine
	eng       gnet.Engine
	loops     int           // event-loop 协程数（≈ 核数），启动后打印
	bootCh    chan struct{} // 通知 main：服务已就绪，可以连了
	closeBoot sync.Once
}

// OnBoot 服务启动时调用一次：保存 engine（后面 Stop 要用），记录 event-loop 数量。
func (s *Server) OnBoot(eng gnet.Engine) gnet.Action {
	s.eng = eng
	log.Printf("[server] booted, event-loops = %d (≈ CPU 核数，管理全部连接)", s.loops)
	s.closeBoot.Do(func() { close(s.bootCh) })
	return gnet.None
}

// OnOpen 新连接建立：建上下文 + 注册进连接管理器。运行在某个 event-loop 协程上。
func (s *Server) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	ctx := &ConnContext{
		ConnId:    genConnId(),
		Addr:      c.RemoteAddr().String(),
		StartTime: time.Now(),
	}
	c.SetContext(ctx)            // 状态挂到连接上，串行访问、无需锁
	clientMgr.Add(ctx.ConnId, c) // 注册，后面按 id 推送靠它找到 c
	return nil, gnet.None
}

// OnTraffic socket 可读时由 event-loop 回调（epoll 就绪驱动）。
// 拆包 → 处理 → 回包，全程绝不阻塞——一旦在这里做阻塞操作(查库/调外部接口)，
// 会卡住该 event-loop 上的所有连接。
func (s *Server) OnTraffic(c gnet.Conn) gnet.Action {
	ctx, ok := c.Context().(*ConnContext)
	if !ok {
		return gnet.Close
	}
	// 一次可读事件里缓冲区可能堆了多个整包，循环取干，直到只剩半个包。
	for {
		packet, err := readFullPacket(c)
		if err != nil {
			return gnet.Close
		}
		if packet == nil {
			return gnet.None // 半个包，等下次 OnTraffic
		}
		rsp := s.process(ctx, packet) // 业务处理（注意：别在这里阻塞！）
		if rsp != nil {
			// 异步回包：丢进 gnet 写队列，框架保证并发安全，不在回调里同步写 socket。
			c.AsyncWrite(encode(rsp), nil)
		}
	}
}

// process 业务逻辑：收到 ping 回 pong。真实场景这里是协议分发，但务必保持非阻塞。
func (s *Server) process(ctx *ConnContext, packet []byte) []byte {
	if string(packet) == "ping" {
		return []byte("pong")
	}
	return nil
}

// OnClose 连接断开：注销 + 清理，状态从 context 里捞。
func (s *Server) OnClose(c gnet.Conn, err error) gnet.Action {
	if ctx, ok := c.Context().(*ConnContext); ok && ctx != nil {
		clientMgr.Remove(ctx.ConnId) // 先注销，别让推送再找到它
		c.SetContext(nil)            // 解引用，帮 GC
	}
	return gnet.None
}

// OnTick 定时回调（需 WithTicker(true) 才会触发）：这里用来周期打印在线连接数，
// 对应真实场景的「心跳超时检查 / 统计上报」。
func (s *Server) OnTick() (delay time.Duration, action gnet.Action) {
	log.Printf("[server] tick: 在线连接 = %d, 当前 goroutine = %d",
		clientMgr.Count(), runtime.NumGoroutine())
	return 500 * time.Millisecond, gnet.None
}

// ──────────────────────── client（自驱动验证用，裸 TCP）────────────────────────────

// client 一条裸 TCP 连接，按同样的「长度前缀」协议收发。
type client struct {
	conn net.Conn
	id   int
}

func dialClient(addr string, id int) (*client, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &client{conn: conn, id: id}, nil
}

// send 发一个整包。
func (cl *client) send(payload []byte) error {
	_, err := cl.conn.Write(encode(payload))
	return err
}

// recvOne 收一个整包：先读满 4 字节包头，再按长度读满包体。
func (cl *client) recvOne() ([]byte, error) {
	hdr := make([]byte, headerLen)
	if _, err := readFull(cl.conn, hdr); err != nil {
		return nil, err
	}
	bodyLen := int(binary.BigEndian.Uint32(hdr))
	body := make([]byte, bodyLen)
	if _, err := readFull(cl.conn, body); err != nil {
		return nil, err
	}
	return body, nil
}

// readFull 把 buf 读满（处理 TCP 短读）。
func readFull(conn net.Conn, buf []byte) (int, error) {
	got := 0
	for got < len(buf) {
		n, err := conn.Read(buf[got:])
		if err != nil {
			return got, err
		}
		got += n
	}
	return got, nil
}

// ───────────────────────────────── main ─────────────────────────────────

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	// 选一个空闲端口（gnet 需要明确地址；先用系统分配再让出，降低冲突概率）。
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	loops := runtime.NumCPU()
	srv := &Server{loops: loops, bootCh: make(chan struct{})}

	// gnet.Run 会阻塞，放到后台协程跑。
	go func() {
		err := gnet.Run(srv, "tcp://"+addr,
			gnet.WithMulticore(true),     // 多 event-loop
			gnet.WithNumEventLoop(loops), // event-loop 数 = 核数
			gnet.WithTicker(true),        // 启用 OnTick
			gnet.WithReuseAddr(true),
		)
		if err != nil {
			log.Printf("[server] gnet.Run exit: %v", err)
		}
	}()

	<-srv.bootCh // 等服务就绪
	log.Printf("[server] listening on %s", addr)

	log.Printf("[main] 连客户端前 goroutine = %d", runtime.NumGoroutine())

	// ── 场景一：海量连接，goroutine 数与连接数解耦 ──
	// 连 200 条连接并保持，每条发一个 ping、收一个 pong。重点观察：服务端 event-loop
	// 协程数固定为「核数」，OnTick 打印的 goroutine 数不会随这 200 条连接线性增长。
	const numConns = 200
	var wg sync.WaitGroup
	clients := make([]*client, 0, numConns)
	var clientsMu sync.Mutex
	for i := 0; i < numConns; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			cl, err := dialClient(addr, id)
			if err != nil {
				log.Printf("[client %d] dial: %v", id, err)
				return
			}
			if err := cl.send([]byte("ping")); err != nil {
				return
			}
			if msg, err := cl.recvOne(); err == nil && id < 3 {
				log.Printf("[client %d] recv: %s", id, msg) // 只打印前几个，免刷屏
			}
			clientsMu.Lock()
			clients = append(clients, cl)
			clientsMu.Unlock()
		}(i)
	}
	wg.Wait()
	log.Printf("[main] 已建立 %d 条连接，服务端在线 = %d", numConns, clientMgr.Count())
	log.Printf("[main] 连 %d 条连接后 goroutine = %d —— 服务端 event-loop 始终 %d 个，"+
		"goroutine 数和连接数解耦（这就是模型三能扛百万长连接的根本）",
		numConns, runtime.NumGoroutine(), loops)

	// ── 场景二：并发推送同一连接，AsyncWrite 保证写安全（-race 干净）──
	// 挑第一条连接，开 5 个业务协程同时往它推送。它们都只调 AsyncWrite 入队，
	// 由 event-loop 串行落地，业务侧无锁、无 data race。
	if len(clients) > 0 {
		target := clients[0] // 挑一条连接，待会读它收到的推送来印证
		// 5 个业务协程对全部在线连接并发广播。它们都只调 AsyncWrite 入队，
		// 同一连接的并发写由 gnet 串行落地，业务侧无锁、-race 干净。
		var pushWg sync.WaitGroup
		ids := snapshotIds()
		for w := 0; w < 5; w++ {
			pushWg.Add(1)
			go func(worker int) {
				defer pushWg.Done()
				for _, id := range ids {
					clientMgr.Push(id, []byte(fmt.Sprintf("server broadcast from worker %d", worker)))
				}
			}(w)
		}
		pushWg.Wait()

		// target 这条连接此刻应收到 5 条 broadcast，读出来印证 AsyncWrite 落地。
		_ = target.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		for i := 0; i < 5; i++ {
			msg, err := target.recvOne()
			if err != nil {
				break
			}
			log.Printf("[client %d] recv push: %s", target.id, msg)
		}
	}

	time.Sleep(600 * time.Millisecond) // 多跑一会，让 OnTick 打印几轮统计

	// 收摊：关掉所有客户端，再停 gnet。
	for _, cl := range clients {
		cl.conn.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if srv.eng.Validate() == nil {
		_ = srv.eng.Stop(ctx)
	}
	log.Printf("[main] demo done")
}

// snapshotIds 拷一份当前在线连接 id 列表，供并发推送遍历。
func snapshotIds() []string {
	clientMgr.mu.RLock()
	defer clientMgr.mu.RUnlock()
	ids := make([]string, 0, len(clientMgr.conns))
	for id := range clientMgr.conns {
		ids = append(ids, id)
	}
	return ids
}
