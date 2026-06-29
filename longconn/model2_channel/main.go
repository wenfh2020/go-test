// 模型二「读写双协程 + channel」的完整可运行 demo。
//
// 对应文章：Go 长连接服务的三种读写并发模型 · §2.2
// https://wenfh2020.com/2026/06/25/go-longconn-concurrency-models/
//
// 程序自带 server + client，同进程自驱动，无需任何外部服务即可运行：
//
//	go run .          普通运行，观察日志
//	go run -race .    开启竞态检测——核心看点：每连接唯一的 writePump 是该连接的
//	                  唯一 writer，读协程(回 pong)、多个推送协程都只往 channel 投递、
//	                  不直接写 ws，所以「写」天然无锁、串行；-race 下干净无告警。
//
// 演示三件事：
//  1. 写收敛到单协程 → 无锁；多个 goroutine 并发 Push，writePump 串行落地。
//  2. channel 带缓冲 → 背压；慢客户端的发送队列被打满，直接被踢，不拖累别人。
//  3. close-once 保护；用 sync.Once + done 通道保证只关一次，投递不会 panic。
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ───────────────────────── 第一层：单连接，双协程 + channel ─────────────────────────

const (
	sendBufSize  = 8                // 发送队列缓冲：满了即判定客户端太慢
	writeWait    = 5 * time.Second  // 单次写超时
	pingInterval = 30 * time.Second // 心跳间隔
)

// Conn 封装一条 websocket 连接：读写各一个 goroutine，写全部收敛到 send 队列。
type Conn struct {
	id        string
	ws        *websocket.Conn
	send      chan []byte   // 带缓冲发送队列，所有要发的消息先进这里
	done      chan struct{} // 关闭信号：广播"这条连接要收摊了"
	closeOnce sync.Once     // 保证 done 只关一次（对应文章「需要 closed 标志保证只关一次」）

	// 仅用于 demo：模拟"慢客户端/假死"。为 true 时 writePump 每写一条都故意 sleep，
	// 让突发推送很快打满 send 队列，从而触发背压踢人。真实代码里没有这个字段。
	slow bool
}

// close 关闭连接：只关 done（永不关 send），所以任何 goroutine 的 Push 都不会
// "向已关闭 channel 发送"而 panic。sync.Once 保证幂等。
//
// 关于「谁能关 channel」：Go 语言层面没有「谁创建谁才能关」的限制，任何持有引用的
// 协程都能 close()。runtime 只兜两条硬规则，违反即 panic：重复关闭、关 nil channel
// （外加「向已关闭 channel 发送」也 panic，接收则永远安全）。社区惯例「由唯一发送方
// 关闭」是为避免别的发送者撞上「向已关闭 channel 发送」，针对的是发送方而非创建方。
// 本文件 done 由 readPump / Push 背压等多个协程关闭——之所以安全，是因为：
//   - done 是「只关、从不发送」的纯信号 channel，多关闭者唯一的风险只是重复关，
//     用 sync.Once 兜住即可彻底安全；
//   - 真正有多个并发发送者(Push)的 send 反而永不 close，靠 done 广播 + drain 取干，
//     连接对象整体被 GC 回收——从设计上消解掉「多发送者不能关」的约束。
func (c *Conn) close() {
	c.closeOnce.Do(func() { close(c.done) })
}

// Push 非阻塞投递：投进队列就返回；队列满 = 客户端太慢 → 背压踢掉。
// 任意业务 goroutine 都能调用，它们只管投递、从不直接写 ws。
//
// 这里的 select 是「带 default 的非阻塞」形态，三件套 send/done/default：
//   - 绝不 panic：close() 只关 done、永不关 send；向已关闭 channel「接收」安全，
//     只有「发送」才 panic，所以并发 Push 永远不会炸。
//   - 绝不阻塞：done 关闭后 <-c.done 永远就绪，select 立刻返回，不卡业务协程。
//   - 返回值不确定（关键）：当 send 还有缓冲位、done 又已关闭时，前两个 case 同时
//     就绪 → Go 在就绪 case 中「伪随机均匀选一个」（case 顺序≠优先级），所以
//     close(done) 后的 Push 可能仍 enqueue 返回 true，也可能返回 false。
//     即 close(done) 并不保证后续 Push 立刻全部停投——这是尽力而为的关闭语义。
//   - default 只在「所有 case 都不就绪」时才走；done 一旦关闭就永远就绪，故连接关闭
//     后背压分支不再触发。想要「关闭严格优先」得手写两段 select 先短路 done。
func (c *Conn) Push(data []byte) bool {
	select {
	case c.send <- data: // 有缓冲位，投递成功
		return true
	case <-c.done: // 已关闭，别再投
		return false
	default: // 队列满 → 慢客户端，踢掉，不拖累别人（背压）
		log.Printf("[server] conn %s send buffer full -> kick (backpressure)", c.id)
		c.close()
		return false
	}
}

// readPump 每连接唯一的读 goroutine：只读，把要回的内容丢进队列，不直接写。
//
// 注意分层：下面 ReadMessage 没数据时，goroutine 并不真的占线程阻塞 read，而是被
// Go runtime 挂到 netpoller 上——Linux 下 netpoller 就是 epoll。成千上万条连接的
// 「socket 可读」由这一个 epoll 实例统一管理，来数据才唤醒对应 readPump。所以 Go 里
// 真正对应 Linux select/epoll 的是这层透明的 netpoller，而不是 select 关键字；
// select 关键字是再上一层、在 channel 之间做多路复用（见 writePump）。
func (c *Conn) readPump(m *ClientManager) {
	defer func() {
		c.close()      // 读出错/对端关闭 → 通知 writePump 收摊
		m.Delete(c.id) // 从连接表摘除
		log.Printf("[server] conn %s readPump exit", c.id)
	}()
	for {
		_, msg, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		if string(msg) == "ping" {
			c.Push([]byte("pong")) // 读协程也只投递，不直接写
		}
	}
}

// writePump 每连接唯一的写 goroutine：本连接唯一 writer，无需锁；顺带统一发心跳。
//
// 这里的 select 是「无 default 的阻塞多路事件循环」：一个协程同时等「有消息要发 /
// 该发心跳 / 连接要收摊」三件事，谁先就绪处理谁；都不就绪就让出 CPU、挂在这几个
// channel 上睡着（不忙轮询）。<-c.done 用「关闭 channel」做一对多广播退出——关闭的
// channel 永远可读，任何监听它的 select 都会被唤醒。注意 send 与 done 同时就绪时
// 仍是随机选；命中 done 后不直接退，而是先 drain 把队列里残余消息尽量发完再走，
// 不丢弃已投递的数据（对应 msggateway 的 loopSend：收到 done 后 drain 残余再退出）。
func (c *Conn) writePump() {
	tick := time.NewTicker(pingInterval)
	defer func() {
		tick.Stop()
		c.ws.Close() // 写循环退出即关闭底层连接
		log.Printf("[server] conn %s writePump exit", c.id)
	}()
	for {
		select {
		case data := <-c.send:
			if err := c.writeText(data); err != nil {
				return
			}
		case <-tick.C:
			c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.done:
			// 连接已被关闭（读出错 / 被背压踢掉）：退出前把队列里残余、已投递但
			// 还没写出的消息尽量发完，不直接丢弃。
			c.drain()
			return
		}
	}
}

// rawWrite 真正把一条业务消息落地，不含任何 demo 模拟。
func (c *Conn) rawWrite(data []byte) error {
	c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	return c.ws.WriteMessage(websocket.TextMessage, data)
}

// writeText 正常写循环用：在 rawWrite 之上叠加 slow demo 的慢写，模拟慢客户端。
// 注意 drain 不走这里、而是直接 rawWrite——关闭时只想尽快冲刷残余，不该再吃慢写延迟。
func (c *Conn) writeText(data []byte) error {
	if c.slow {
		time.Sleep(40 * time.Millisecond) // demo：仅正常写循环里模拟慢客户端
	}
	return c.rawWrite(data)
}

// drain 在连接关闭后，非阻塞地把 send 队列里残余的消息尽量发完再退出。
// send 永不被 close（只关 done），所以这里用 select-default 取干为止：
// 取到就写，取不到（队列空）就立即返回，绝不阻塞。
// 用 rawWrite 而非 writeText：被背压踢掉的恰恰是 slow 连接，关闭时若再吃 40ms/条的
// 慢写延迟，等于「刚因为它慢把它踢了，又慢吞吞往它灌」，自相矛盾且拖长 teardown。
func (c *Conn) drain() {
	for {
		select {
		case data := <-c.send:
			if err := c.rawWrite(data); err != nil {
				return
			}
		default:
			log.Printf("[server] conn %s drained, writePump closing", c.id)
			return
		}
	}
}

// ───────────────────────── 第二层：连接表，分片加锁 ─────────────────────────

const shardCount = 32

// ClientManager 分片连接表：按 key 哈希散到 N 个小 map，每片各一把锁。
type ClientManager struct {
	shards [shardCount]*shard
}

type shard struct {
	mu    sync.RWMutex
	conns map[string]*Conn
}

func NewClientManager() *ClientManager {
	m := &ClientManager{}
	for i := range m.shards {
		m.shards[i] = &shard{conns: make(map[string]*Conn)}
	}
	return m
}

// getShard 用 FNV-1a 哈希把 key 稳定散到某一片。
func (m *ClientManager) getShard(key string) *shard {
	h := uint32(2166136261)
	for i := 0; i < len(key); i++ {
		h = (h ^ uint32(key[i])) * 16777619
	}
	return m.shards[h%shardCount]
}

func (m *ClientManager) Get(key string) (*Conn, bool) {
	s := m.getShard(key)
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.conns[key]
	return c, ok
}

func (m *ClientManager) Set(key string, c *Conn) {
	s := m.getShard(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns[key] = c
}

func (m *ClientManager) Delete(key string) {
	s := m.getShard(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, key)
}

// Push 按 id 找到连接去推送：只是把数据投进它的 send 队列，写由各自的 writePump 负责。
func (m *ClientManager) Push(id string, data []byte) {
	if c, ok := m.Get(id); ok {
		c.Push(data)
	}
}

// ───────────────────────────────── server ─────────────────────────────────

var upgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true },
}

func (m *ClientManager) handleWS(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	id := r.URL.Query().Get("id")
	c := &Conn{
		id:   id,
		ws:   ws,
		send: make(chan []byte, sendBufSize),
		done: make(chan struct{}),
		slow: id == "slow-1", // demo：slow-1 这条连接模拟慢客户端
	}
	m.Set(id, c)
	go c.readPump(m)  // 读协程
	go c.writePump()  // 写协程（本连接唯一 writer）

	// 起 3 个推送协程，和 readPump 的 pong 一起并发 Push 同一连接——它们都只投递，
	// 真正落地全靠唯一的 writePump 串行完成，所以无锁、-race 干净。
	for i := 0; i < 3; i++ {
		go func(worker int) {
			for j := 0; j < 3; j++ {
				c.Push([]byte(fmt.Sprintf("push msg#%d from worker %d", j, worker)))
				time.Sleep(50 * time.Millisecond)
			}
		}(i)
	}
}

// ──────────────────────── client（自驱动验证用）────────────────────────────

// runClient 普通客户端：及时收消息，发几个 ping 触发 server 回 pong。
func runClient(addr, id string) {
	url := fmt.Sprintf("ws://%s/ws?id=%s", addr, id)
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Fatalf("[client %s] dial: %v", id, err)
	}
	defer ws.Close()

	go func() {
		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				return
			}
			log.Printf("[client %s] recv: %s", id, msg)
		}
	}()

	for i := 0; i < 3; i++ {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("ping"))
		time.Sleep(120 * time.Millisecond)
	}
	time.Sleep(400 * time.Millisecond) // 等剩余推送收完
}

// runSlowClient 慢客户端：连上后基本不读（读得极慢），server 一突发推送，
// 它的 send 队列很快打满，被背压踢掉——这正是模型二相对模型一的关键优势。
func runSlowClient(addr, id string, mgr *ClientManager) {
	url := fmt.Sprintf("ws://%s/ws?id=%s", addr, id)
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Fatalf("[client %s] dial: %v", id, err)
	}
	defer ws.Close()

	// 故意不起读协程：客户端不收，叠加 server 端 writePump 的模拟慢写，
	// 队列迅速被打满。等 server 把这条连接登记好，再猛灌一批推送。
	time.Sleep(150 * time.Millisecond)
	for i := 0; i < 50; i++ {
		mgr.Push(id, []byte(fmt.Sprintf("burst push #%d", i)))
	}
	time.Sleep(600 * time.Millisecond) // 留时间看背压踢人日志
}

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	// 随机端口避免冲突；server 与 client 同进程，demo 完全自包含。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	addr := ln.Addr().String()

	mgr := NewClientManager()
	srv := &http.Server{Handler: http.HandlerFunc(mgr.handleWS)}
	go srv.Serve(ln)
	log.Printf("[server] listening on %s", addr)

	// 场景一：正常客户端 —— 并发写收敛到单 writePump，-race 干净。
	const normalID = "visitor-1"
	go runClient(addr, normalID)

	// server 侧演示连接表用途：稍后按 id 主动找连接推送（同样只是投进队列）。
	time.Sleep(200 * time.Millisecond)
	mgr.Push(normalID, []byte("server push by id (via ClientManager)"))

	// 场景二：慢客户端 —— 队列打满触发背压，被踢，不拖累正常连接。
	go runSlowClient(addr, "slow-1", mgr)

	time.Sleep(1500 * time.Millisecond) // 跑够时间收完所有消息 + 观察背压
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	log.Printf("[main] demo done")
}
