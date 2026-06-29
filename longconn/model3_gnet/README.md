# 模型三：事件驱动（gnet / reactor）

配套文章《Go 长连接服务的三种读写并发模型》 §2.3 的完整可运行 demo。

文章：<https://wenfh2020.com/2026/06/25/go-longconn-concurrency-models/>

## 它演示了什么

没有「每连接一个读协程」。所有 fd 挂在 epoll 上，由少量 event-loop 协程（≈ CPU 核数）统一管理；内核说哪个 fd 可读，就回调谁的 `OnTraffic`。在一个自驱动（server + client 同进程）的 demo 里跑通三件事：

- **goroutine 数和连接数解耦**：连上 **200** 条连接前后，进程 goroutine 数稳定在 ~14（= 核数个 event-loop + 少量运行时协程），**完全不随连接数增长**。这正是模型三能扛百万长连接的根本——对比模型一/二，连接数 ×N 就是 goroutine ×N。
- **异步写、框架保证并发安全**：5 个业务协程对同一批连接并发 `AsyncWrite` 广播，数据丢进 gnet 写队列由 event-loop 串行落地，业务侧**无需任何锁**，`-race` 全程干净。
- **自己粘包拆包**：gnet 工作在裸 TCP 上、没有 websocket 的消息边界，所以用「4 字节大端长度前缀 + 包体」协议（`readFullPacket`），一次 `OnTraffic` 里循环把缓冲区的整包取干，半个包就留到下次。

## 运行

本目录是独立 module，依赖 [gnet/v2](https://github.com/panjf2000/gnet)：

```sh
go run .          # 普通运行，观察日志
go run -race .    # 开竞态检测 —— 核心看点：并发 AsyncWrite 同一连接，全程干净无告警
```

## 预期输出（节选）

```
[server] booted, event-loops = 8 (≈ CPU 核数，管理全部连接)
[main] 连客户端前 goroutine = 14
[main] 已建立 200 条连接，服务端在线 = 200
[main] 连 200 条连接后 goroutine = 14 —— 服务端 event-loop 始终 8 个，goroutine 数和连接数解耦
[client 4] recv push: server broadcast from worker 1
[server] tick: 在线连接 = 200, 当前 goroutine = 14
[main] demo done
```

注意两行 goroutine 数：**0 条连接和 200 条连接时都是 14**。这就是 reactor 用 epoll 取代「每连接协程」的意义——守连接的不再是 N 个协程，而是固定的 ≈ 核数个 event-loop。

## gnet 回调对照（见文章 §2.3 表）

| 回调        | 触发时机             | 本 demo 里干了什么                       |
| :---------- | :------------------- | :--------------------------------------- |
| `OnBoot`    | 服务启动一次         | 保存 engine、记录 event-loop 数          |
| `OnOpen`    | 新连接建立           | 建 `ConnContext`、注册进 `ClientManager` |
| `OnTraffic` | socket 可读（epoll） | 循环拆包 → 处理 ping → `AsyncWrite` 回 pong |
| `OnClose`   | 连接断开             | 从 `ClientManager` 注销、`SetContext(nil)` |
| `OnTick`    | 定时器周期触发       | 打印在线连接数 + 当前 goroutine 数       |

## 几个关键坑（文章「缺点」对应）

- **`OnTraffic` 里绝不能阻塞**：查库、调外部接口会卡住该 event-loop 上的**所有**连接。demo 的 `process` 是纯内存逻辑。
- **`Next`/`Peek` 的切片会被复用**：它们指向 gnet 内部缓冲，回调返回后即被串改。`readFullPacket` 把包体 `copy` 一份再交出去。
- **写用 `AsyncWrite` 而非同步写**：把数据入队、由框架串行落地，既保证并发安全，又不在回调里阻塞。
- **连接状态挂 `c.SetContext`**：同一连接所有回调跑在同一 event-loop 协程、彼此串行，所以读写 `ConnContext` 天然无需加锁；要加锁的只是跨连接的全局 `ClientManager`。

## 与文章代码片段的差异

文章片段用 `genUUID()`、`clearCache()` 等示意函数，省略了协议细节。本 demo 用最简单的「长度前缀」协议补全了 `readFullPacket` 的真实粘包拆包，并加了一个同进程裸 TCP 客户端做自驱动验证。
