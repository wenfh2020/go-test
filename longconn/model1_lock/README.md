# 模型一：单读协程 + 加锁直写

配套文章《Go 长连接服务的三种读写并发模型》 §2.1 的完整可运行 demo。

文章：<https://wenfh2020.com/2026/06/25/go-longconn-concurrency-models/>

## 它演示了什么

模型一的两层加锁，在一个自驱动（server + client 同进程）的 demo 里跑通：

- **单连接，加锁直写**：同一条连接被「读协程回 pong」和「3 个推送协程」并发写，全靠每连接一把 `sync.Mutex` 串行化（`Conn.Write`）。
- **连接表，分片加锁**：连接注册进 32 分片的 `ClientManager`，演示「按 id 找到连接去推送」（`ClientManager.Push`）以及断开即摘除。

## 运行

本目录是独立 module，无需外部服务：

```sh
go run .          # 普通运行，观察日志
go run -race .    # 开竞态检测 —— 核心看点：全程干净无告警
```

核心卖点是 `-race`：同一连接被多个 goroutine 并发写却没有 data race，正是每连接写锁的功劳。把 `Conn.Write` 里的 `c.mu.Lock()/Unlock()` 注释掉再 `go run -race .`，会立刻报 `DATA RACE`。

## 预期输出（节选）

```
[server] listening on 127.0.0.1:xxxxx
[client] recv: push msg#0 from worker 1
[client] recv: pong
[client] recv: server push by id (via ClientManager)
[server] conn visitor-1 readLoop exit, removed from manager
[main] demo done
```
