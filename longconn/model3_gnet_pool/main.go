// 模型三·进阶「事件驱动（gnet） + 协程池卸载阻塞业务」的完整可运行 demo。
//
// 对应文章：Go 长连接服务的三种读写并发模型 · §2.3
// https://wenfh2020.com/2026/06/25/go-longconn-concurrency-models/
//
// 与 model3_gnet 的区别：model3_gnet 的 process() 是纯内存、非阻塞的，只演示
// 「并发 AsyncWrite 同一连接的写安全」。但真实业务里 OnTraffic 常要查 redis/mysql、
// 跑复杂逻辑——这些是【阻塞操作】，一旦放进 OnTraffic 就会卡住该 event-loop 上的
// 【所有】连接。本 demo 演示正确姿势：
//
//	event-loop 只做「非阻塞拆包 + 派发」，阻塞业务甩给【有界协程池】，
//	处理完用 AsyncWrite 异步回包。
//
//	go run .          普通运行，观察四个场景日志
//	go run -race .    竞态检测——核心看点：worker 协程并发 AsyncWrite，且与 OnClose
//	                  存在天然竞态，全靠 gnet「conn 不复用 + AsyncWrite 在 event-loop
//	                  串行落地、执行时查 opened」保证安全，业务侧无锁、-race 干净。
//
// ── 并发安全为什么成立（已读 gnet v2.9.8 源码验证）──
//  1. gnet 不复用 *conn 对象（每条连接 newStreamConn 都是全新 &conn{}，release 只置空
//     不入池）。故 worker 里握着的 c 永远是同一条逻辑连接，【不可能串台】——因此回包
//     直接用捕获的 c 即可，无需拿 ConnId 重查连接表来防串台。
//  2. AsyncWrite 把写任务投递到该连接所属 event-loop，在【执行时刻】检查
//     `if !c.opened { return net.ErrClosed }`，与 OnClose 在同一 event-loop 串行。
//     故 worker 处理途中连接被关，最坏是回调里拿到 net.ErrClosed，安全丢弃。
//  3. 所以真正要做的并发安全动作只有：拆包后 copy、用有界池而非裸 go、AsyncWrite 用
//     callback 观察 err（而非看返回值，返回值只表示入队成败）、注意同连接多包并发的乱序。
//
// ── 协议 ──
// gnet 工作在裸 TCP 上，没有消息边界，需自己粘包拆包。用最简单的
// 「4 字节大端长度前缀 + 包体」协议：[len:4][payload:len]。
package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"log"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/panjf2000/ants/v2"
	"github.com/panjf2000/gnet/v2"
)

// ───────────────────────── 协议：4 字节长度前缀粘包拆包 ─────────────────────────

const headerLen = 4 // 包头：4 字节大端，表示后面包体的字节数

// encode 把一段 payload 封成 [len:4][payload] 的整包。
func encode(payload []byte) []byte {
	buf := make([]byte, headerLen+len(payload))
	binary.BigEndian.PutUint32(buf, uint32(len(payload)))
	copy(buf[headerLen:], payload)
	return buf
}

// readFullPacket 从 gnet 入站缓冲里「凑齐一个整包」就取出来，凑不齐返回 nil。
//
// 关键坑：c.Next/c.Peek 返回的切片指向 gnet 内部缓冲，OnTraffic 回调返回后会被复用。
// 本 demo 要把包体丢给协程池（跨协程、且延后处理），所以【必须 copy 一份】再交出去，
// 否则 worker 读到的会是被后续数据串改的脏内容。
func readFullPacket(c gnet.Conn) ([]byte, error) {
	if c.InboundBuffered() < headerLen {
		return nil, nil // 连包头都没凑齐
	}
	hdr, _ := c.Peek(headerLen)
	bodyLen := int(binary.BigEndian.Uint32(hdr))
	if c.InboundBuffered() < headerLen+bodyLen {
		return nil, nil // 包头到了，包体还没齐
	}
	if _, err := c.Discard(headerLen); err != nil {
		return nil, err
	}
	body, err := c.Next(bodyLen)
	if err != nil {
		return nil, err
	}
	out := make([]byte, bodyLen) // copy，脱离 gnet 内部缓冲
	copy(out, body)
	return out, nil
}

// ───────────────────────── 每连接上下文 ─────────────────────────

// ConnContext 每条连接的状态，挂在 gnet.Conn 的 context 上。
// 同一连接的所有回调(OnOpen/OnTraffic/OnClose)跑在同一 event-loop 协程、彼此串行，
// 所以在回调里读写它无需加锁。但 worker 协程也会读它，故：worker 里只【只读】不变字段
// (ConnId)，或用 atomic(closed)——绝不在 worker 里改会和 event-loop 竞争的字段。
type ConnContext struct {
	ConnId    string
	Addr      string
	StartTime time.Time
	closed    atomic.Bool // OnClose 里置 true；worker 取包后先看一眼，已关就跳过（纯优化，非防串台）
}

var connSeq uint64

func genConnId() string {
	return fmt.Sprintf("conn-%d", atomic.AddUint64(&connSeq, 1))
}

// ───────────────────────── 分片协程池（ants 承载）─────────────────────────
//
// 设计：numShards 条带缓冲的任务 channel，每条由 ants 池里的一个常驻 worker goroutine
// 串行消费。按 ConnId 哈希把「同一连接」恒定路由到「同一 shard」——于是：
//   - 同连接的包被同一 goroutine FIFO 串行处理 → 回包【严格有序】；
//   - 不同连接分散到不同 shard → 【并行】，并发度封顶为 numShards；
//   - OnTraffic 只做非阻塞 channel send → 【event-loop 永不阻塞】；
//   - channel 满 → select-default 非阻塞回退 → 【背压】，绝不把 event-loop 堵在 send 上。
//
// ants 在这里的角色 = 提供【固定数量、受管理】的 worker goroutine（承载 numShards 个
// 常驻 drainLoop）。

type job struct {
	c       gnet.Conn
	ctx     *ConnContext
	payload []byte
}

const shardQueueCap = 256 // 每个 shard 的任务队列容量，满了触发背压

// 模拟一次阻塞的业务处理（查 redis/mysql / 复杂逻辑）耗时。放进 OnTraffic 会卡死
// event-loop，正因如此才要甩给协程池。
const processDelay = 50 * time.Millisecond

var (
	numShards  int
	shards     []chan job
	workerPool *ants.Pool
	shutdownCh = make(chan struct{}) // 关闭信号，让 drainLoop 尽快退出，避免关服时被残余队列拖住

	processedCount   atomic.Int64 // 已处理任务数
	overloadCount    atomic.Int64 // 队列满被丢弃的任务数（背压）
	writeClosedCount atomic.Int64 // AsyncWrite 回调里拿到 err（连接已关）的次数
	onlineCount      atomic.Int64 // 在线连接数
)

// initPool 建分片池：numShards 条队列 + ants 承载 numShards 个常驻消费循环。
func initPool() {
	numShards = runtime.NumCPU()
	shards = make([]chan job, numShards)
	pool, err := ants.NewPool(numShards) // 池大小 = 分片数，正好每个 drainLoop 一个 worker
	if err != nil {
		log.Fatalf("ants.NewPool: %v", err)
	}
	workerPool = pool
	for i := 0; i < numShards; i++ {
		ch := make(chan job, shardQueueCap)
		shards[i] = ch
		if err := workerPool.Submit(func() { drainLoop(ch) }); err != nil {
			log.Fatalf("submit drainLoop: %v", err)
		}
	}
	log.Printf("[pool] 分片协程池就绪：shards = %d, 每分片队列容量 = %d, 单包模拟阻塞 = %v",
		numShards, shardQueueCap, processDelay)
}

// shardOf 按 ConnId 哈希选 shard——同一 ConnId 恒定落到同一 shard。
func shardOf(connId string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(connId))
	return int(h.Sum32() % uint32(numShards))
}

// dispatch 由 event-loop 在 OnTraffic 里调用：非阻塞地把任务投进对应 shard 队列。
// 满了就走 default（背压），绝不阻塞 event-loop。
func dispatch(c gnet.Conn, ctx *ConnContext, payload []byte) {
	ch := shards[shardOf(ctx.ConnId)]
	select {
	case ch <- job{c: c, ctx: ctx, payload: payload}:
	default:
		overloadCount.Add(1) // 池/队列满：这里选择丢弃；真实场景可回“忙”、限流或关连接
	}
}

// drainLoop 一个 shard 的常驻消费循环：串行取包 → 阻塞处理 → 异步回包。
func drainLoop(ch chan job) {
	for {
		// 先非阻塞看一眼关闭信号，保证关服时能尽快退出（否则可能被残余队列 + 50ms/包拖很久）。
		select {
		case <-shutdownCh:
			return
		default:
		}
		select {
		case <-shutdownCh:
			return
		case j, ok := <-ch:
			if !ok {
				return
			}
			if j.ctx.closed.Load() {
				continue // 连接已关，省掉这次无用处理（优化，非安全必需）
			}
			rsp := process(j.ctx, j.payload) // 阻塞业务：在 worker 上跑，不碍 event-loop
			if rsp != nil {
				// 直接用捕获的 c 回包即可（gnet 不复用 conn，不会串台）；
				// 用 callback 观察 err：连接若在处理途中被关，这里会拿到 net.ErrClosed。
				_ = j.c.AsyncWrite(encode(rsp), onWriteDone)
			}
		}
	}
}

// process 模拟一次阻塞的业务处理，然后把 payload 原样回显（echo）。
// echo 便于「同连接严格有序」场景校验：客户端连发 msg-1..msg-K，应按序收回 msg-1..msg-K。
func process(_ *ConnContext, payload []byte) []byte {
	time.Sleep(processDelay) // 冒充 redis/mysql 查询等阻塞调用
	processedCount.Add(1)
	out := make([]byte, len(payload))
	copy(out, payload)
	return out
}

// onWriteDone AsyncWrite 的完成回调。err != nil（如 net.ErrClosed）说明连接已关、
// 回包被安全丢弃——这正是 worker 与 OnClose 竞态下框架的兜底，不 panic、不串台。
func onWriteDone(_ gnet.Conn, err error) error {
	if err != nil {
		writeClosedCount.Add(1)
	}
	return nil
}

// ───────────────────────────────── server（gnet 事件回调）─────────────────────────

type Server struct {
	gnet.BuiltinEventEngine
	eng       gnet.Engine
	loops     int
	bootCh    chan struct{}
	closeBoot sync.Once
}

func (s *Server) OnBoot(eng gnet.Engine) gnet.Action {
	s.eng = eng
	log.Printf("[server] booted, event-loops = %d (≈ CPU 核数，管理全部连接)", s.loops)
	s.closeBoot.Do(func() { close(s.bootCh) })
	return gnet.None
}

func (s *Server) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	ctx := &ConnContext{
		ConnId:    genConnId(),
		Addr:      c.RemoteAddr().String(),
		StartTime: time.Now(),
	}
	c.SetContext(ctx)
	onlineCount.Add(1)
	return nil, gnet.None
}

// OnTraffic socket 可读：只做「拆包 + 派发」，全程非阻塞——把阻塞业务甩给协程池。
func (s *Server) OnTraffic(c gnet.Conn) gnet.Action {
	ctx, ok := c.Context().(*ConnContext)
	if !ok {
		return gnet.Close
	}
	for { // 一次可读事件里可能堆了多个整包，循环取干
		packet, err := readFullPacket(c)
		if err != nil {
			return gnet.Close
		}
		if packet == nil {
			return gnet.None // 半个包，等下次 OnTraffic
		}
		dispatch(c, ctx, packet) // 不在这里处理！投给协程池，event-loop 立刻回来收下一个包
	}
}

func (s *Server) OnClose(c gnet.Conn, err error) gnet.Action {
	if ctx, ok := c.Context().(*ConnContext); ok && ctx != nil {
		ctx.closed.Store(true) // 通知 worker：这条连接已关，别再白干
		c.SetContext(nil)      // 解引用，帮 GC
	}
	onlineCount.Add(-1)
	return gnet.None
}

func (s *Server) OnTick() (delay time.Duration, action gnet.Action) {
	log.Printf("[server] tick: 在线连接 = %d, 当前 goroutine = %d, 已处理 = %d",
		onlineCount.Load(), runtime.NumGoroutine(), processedCount.Load())
	return 300 * time.Millisecond, gnet.None
}

// ──────────────────────── client（自驱动验证用，裸 TCP）────────────────────────────

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

func (cl *client) send(payload []byte) error {
	_, err := cl.conn.Write(encode(payload))
	return err
}

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

	initPool()

	// 选一个空闲端口。
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	loops := runtime.NumCPU()
	srv := &Server{loops: loops, bootCh: make(chan struct{})}
	go func() {
		err := gnet.Run(srv, "tcp://"+addr,
			gnet.WithMulticore(true),
			gnet.WithNumEventLoop(loops),
			gnet.WithTicker(true),
			gnet.WithReuseAddr(true),
		)
		if err != nil {
			log.Printf("[server] gnet.Run exit: %v", err)
		}
	}()
	<-srv.bootCh
	log.Printf("[server] listening on %s", addr)
	log.Printf("[main] 起步 goroutine = %d（含 %d event-loop + %d worker + 运行时协程）",
		runtime.NumGoroutine(), loops, numShards)

	// ── 场景 A/B：200 条连接并发 ping，业务阻塞 50ms，靠协程池并行消化 ──
	// 看点1：即便业务阻塞，event-loop goroutine 数仍固定（≈核数 + 固定 worker），与连接数解耦。
	// 看点2：总耗时 ≈ ceil(200/numShards)*50ms（池并行），而非串行 200*50ms=10s。
	const numConns = 200
	conns := make([]*client, 0, numConns)
	for i := 0; i < numConns; i++ {
		cl, err := dialClient(addr, i)
		if err != nil {
			log.Fatalf("dial: %v", err)
		}
		conns = append(conns, cl)
	}
	log.Printf("[main] 已建立 %d 条连接，服务端在线 = %d", numConns, onlineCount.Load())

	start := time.Now()
	var wg sync.WaitGroup
	for _, cl := range conns {
		wg.Add(1)
		go func(cl *client) { // 这些 client 协程发完收完即退出，不常驻
			defer wg.Done()
			if err := cl.send([]byte("ping")); err != nil {
				return
			}
			_, _ = cl.recvOne()
		}(cl)
	}
	wg.Wait()
	elapsed := time.Since(start)
	serialEst := time.Duration(numConns) * processDelay
	poolEst := time.Duration((numConns+numShards-1)/numShards) * processDelay
	log.Printf("[场景B] %d 条 ping 全部处理完：实际耗时 = %v（串行需 ~%v，池并行理论 ~%v）",
		numConns, elapsed.Round(time.Millisecond), serialEst, poolEst)
	log.Printf("[场景A] 处理这 %d 条阻塞请求期间 goroutine = %d —— event-loop(%d)+worker(%d) 固定，"+
		"不随连接数涨（client 协程已退出）", numConns, runtime.NumGoroutine(), loops, numShards)

	// ── 场景 C：同一连接快速连发 msg-1..msg-K，校验回包严格有序 ──
	// 同 ConnId 恒定哈希到同一 shard → 单 goroutine FIFO 串行 → 顺序不乱。
	const K = 10
	oc, err := dialClient(addr, 1000)
	if err != nil {
		log.Fatalf("dial ordering conn: %v", err)
	}
	for i := 1; i <= K; i++ {
		if err := oc.send([]byte(fmt.Sprintf("msg-%d", i))); err != nil {
			log.Fatalf("send ordering: %v", err)
		}
	}
	ordered := true
	for i := 1; i <= K; i++ {
		msg, err := oc.recvOne()
		if err != nil {
			log.Printf("[场景C] recv err: %v", err)
			ordered = false
			break
		}
		if string(msg) != fmt.Sprintf("msg-%d", i) {
			log.Printf("[场景C] 乱序！期望 msg-%d, 实际 %s", i, msg)
			ordered = false
		}
	}
	if ordered {
		log.Printf("[场景C] 同连接连发 %d 包，回包严格有序 msg-1..msg-%d ✓（哈希到固定 worker 串行）", K, K)
	}
	oc.conn.Close()

	// ── 场景 D：单连接猛灌，把某 shard 队列打满，触发背压丢弃 ──
	const flood = 500
	fc, err := dialClient(addr, 2000)
	if err != nil {
		log.Fatalf("dial flood conn: %v", err)
	}
	for i := 0; i < flood; i++ {
		if err := fc.send([]byte(fmt.Sprintf("flood-%d", i))); err != nil {
			break
		}
	}
	time.Sleep(300 * time.Millisecond) // 让 OnTraffic 把这波包读完、派发到同一 shard
	log.Printf("[场景D] 单连接猛灌 %d 包（都落同一 shard，队列容量 %d）：背压丢弃 = %d 个",
		flood, shardQueueCap, overloadCount.Load())
	fc.conn.Close()

	time.Sleep(400 * time.Millisecond) // 让 OnTick 多打印几轮

	// ── 收摊（顺序很关键，避免向已关资源写）──
	// 1) 关所有 client → 触发 OnClose  2) 停 gnet（不再有 OnTraffic/dispatch）
	// 3) 关 shutdownCh 让 worker 尽快退出  4) 释放 ants 池
	for _, cl := range conns {
		cl.conn.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if srv.eng.Validate() == nil {
		_ = srv.eng.Stop(ctx)
	}
	close(shutdownCh)
	workerPool.Release()

	log.Printf("[main] 汇总：已处理 = %d, 背压丢弃 = %d, 回包遇连接已关 = %d",
		processedCount.Load(), overloadCount.Load(), writeClosedCount.Load())
	log.Printf("[main] demo done")
}
