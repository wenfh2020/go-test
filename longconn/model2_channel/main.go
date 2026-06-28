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
func (c *Conn) close() {
	c.closeOnce.Do(func() { close(c.done) })
}

// Push 非阻塞投递：投进队列就返回；队列满 = 客户端太慢 → 背压踢掉。
// 任意业务 goroutine 都能调用，它们只管投递、从不直接写 ws。
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
			if c.slow {
				time.Sleep(40 * time.Millisecond) // demo：模拟慢客户端，写得很慢
			}
			c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-tick.C:
			c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.done:
			return // 连接已被关闭（读出错 / 被背压踢掉）
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
