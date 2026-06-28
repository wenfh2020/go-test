# 模型二：读写双协程 + channel

配套文章《Go 长连接服务的三种读写并发模型》 §2.2 的完整可运行 demo。

文章：<https://wenfh2020.com/2026/06/25/go-longconn-concurrency-models/>

## 它演示了什么

每条连接起 **2 个** goroutine：`readPump` 只读、`writePump` 只写。所有要发的消息先进一个带缓冲的 `send` channel，由唯一的 `writePump` 取出来写。在一个自驱动（server + client 同进程）的 demo 里跑通三件事：

- **写收敛到单协程 → 无锁**：读协程（回 pong）、3 个推送协程、按 id 的服务端推送，全部只往 `send` 投递、从不直接写 ws；真正落地由唯一的 `writePump` 串行完成，所以 `-race` 全程干净。
- **channel 带缓冲 → 背压**：`slow-1` 模拟慢客户端（不读 + 写得慢），服务端一突发推送就把它的 `send` 队列打满，`Push` 命中 `default` 直接把它踢掉，**不拖累正常连接**。
- **close-once 保护**：用 `sync.Once` + `done` 通道保证只关一次，且永不关 `send`，所以任何 goroutine 投递都不会「向已关闭 channel 发送」而 panic。

## 运行

本目录是独立 module，无需外部服务：

```sh
go run .          # 普通运行，观察日志
go run -race .    # 开竞态检测 —— 核心看点：全程干净无告警
```

`-race` 的看点：同一连接被多个 goroutine 并发投递，却没有任何 data race —— 因为「写」只发生在唯一的 `writePump` 里，这正是「单写协程」替代「锁」的意义。

## 预期输出（节选）

```
[server] listening on 127.0.0.1:xxxxx
[client visitor-1] recv: push msg#0 from worker 2
[client visitor-1] recv: pong
[client visitor-1] recv: server push by id (via ClientManager)
[server] conn slow-1 send buffer full -> kick (backpressure)   <- 背压踢人
[server] conn slow-1 writePump exit
[main] demo done
```

注意 `slow-1` 被踢，而 `visitor-1` 不受任何影响 —— 慢客户端被隔离在自己的队列里，这是模型二相对模型一（加锁直写、阻塞会被锁放大）的关键优势。

## 与文章代码片段的差异

文章片段为了简洁，直接 `close(c.send)`。本 demo 用 `sync.Once` + `done` 通道（只关 `done`、永不关 `send`）来实现「只关一次且投递不 panic」，正是文章「缺点」里提到的 *closed 标志保证只关一次* 的健壮写法。
