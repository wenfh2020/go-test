// 模型一「单读协程 + 加锁直写」的完整可运行 demo。
//
// 对应文章：Go 长连接服务的三种读写并发模型 · §2.1
// https://wenfh2020.com/2026/06/25/go-longconn-concurrency-models/
//
// 程序自带 server + client，同进程自驱动，无需任何外部服务即可运行：
//
//	go run .          普通运行，观察日志
//	go run -race .    开启竞态检测——核心看点：同一条连接被读协程(回 pong)
//	                  和多个推送协程并发写，全靠每连接一把 sync.Mutex 串行化，
//	                  -race 下干净无告警；若注释掉 Write 里的加锁，立刻报 data race。
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

// ───────────────────────── 第一层：单连接，加锁直写 ─────────────────────────

// Conn 封装一条 websocket 连接，每连接一把写锁。
type Conn struct {
	id string
	ws *websocket.Conn
	mu sync.Mutex // 每连接一把写锁，保证同一连接同一时刻只有一个 writer
}

// Write 任意 goroutine 都能调用，靠锁保证同一连接不并发写。
func (c *Conn) Write(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ws.SetWriteDeadline(time.Now().Add(5 * time.Second)) // 本次写限时 5 秒；每次写都重设，无需复位
	return c.ws.WriteMessage(websocket.TextMessage, data)
}

// readLoop 每连接唯一的读 goroutine：阻塞 Read，收到 ping 就加锁回 pong。
func (c *Conn) readLoop(m *ClientManager) {
	defer func() {
		c.ws.Close()
		m.Delete(c.id) // 断开即从连接表摘除
		log.Printf("[server] conn %s readLoop exit, removed from manager", c.id)
	}()
	for {
		_, msg, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		if string(msg) == "ping" {
			_ = c.Write([]byte("pong")) // 读协程里也走加锁写
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

// Push 按 id 找到连接去推送，找不到（已断开/被摘除）就不发。
func (m *ClientManager) Push(id string, data []byte) {
	if c, ok := m.Get(id); ok {
		_ = c.Write(data) // 仍靠每连接的写锁保证同一连接不并发写
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
	c := &Conn{id: id, ws: ws}
	m.Set(id, c)     // 注册进分片连接表
	go c.readLoop(m) // 唯一读协程

	// 起 3 个推送协程，和 readLoop 的 pong 抢着往同一连接写——全靠 c.mu 串行化。
	for i := 0; i < 3; i++ {
		go func(worker int) {
			for j := 0; j < 3; j++ {
				_ = c.Write([]byte(fmt.Sprintf("push msg#%d from worker %d", j, worker)))
				time.Sleep(50 * time.Millisecond)
			}
		}(i)
	}
}

// ──────────────────────── client（自驱动验证用）────────────────────────────

func runClient(addr, id string) {
	url := fmt.Sprintf("ws://%s/ws?id=%s", addr, id)
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Fatalf("[client] dial: %v", err)
	}
	defer ws.Close()

	// 读协程：打印 server 推送回来的所有消息（push / pong / 按 id 推送）。
	go func() {
		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				return
			}
			log.Printf("[client] recv: %s", msg)
		}
	}()

	// 发 3 个 ping，触发 server readLoop 加锁回 pong。
	for i := 0; i < 3; i++ {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("ping"))
		time.Sleep(120 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond) // 等剩余推送收完
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

	const id = "visitor-1"
	go runClient(addr, id)

	// server 侧演示分片表用途：稍后按 id 主动找连接推送。
	time.Sleep(200 * time.Millisecond)
	mgr.Push(id, []byte("server push by id (via ClientManager)"))

	time.Sleep(1 * time.Second) // 跑够时间收完所有消息
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	log.Printf("[main] demo done")
}
