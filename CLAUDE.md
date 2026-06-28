# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 仓库性质

这是一个个人 Go 学习/实验代码集合（"golang test code"）。它**不是**单一应用程序——每个叶子目录都是一个独立的程序（大多为 `package main`），各自探索一个主题：TCP/IM 协议、Redis 用法、ZooKeeper + Viper 配置中心、goroutine/channel 并发、反射，以及 `test/` 下的语言特性演示。

## 构建系统：GOPATH，而非 Go Modules

仓库中**没有 `go.mod`**。尽管 `GO111MODULE=on`，代码使用的是 GOPATH 风格的导入路径，如 `go-test/common` 和 `go-test/storage/cache`。要让这些内部导入能够解析，仓库**必须检出（或软链接）到 `$GOPATH/src/go-test`**。从任意路径构建都会因无法解析 `go-test/...` 导入而失败。

常用命令（需在 `$GOPATH/src/go-test` 目录内执行）：

```sh
# 构建/运行单个程序（每个叶子目录都是独立的 main 包）
go run ./redis/pressure
go build ./server_tcp_proto/imserver

# 静态检查/格式化
go vet ./...
gofmt -l .
```

大多数程序需要依赖**外部服务**才能正常运行：Redis 服务器（`storage/cache`、`redis/...`，默认 `127.0.0.1:6379`）、TCP 对端（`server_tcp_proto`、`test/tcp`——需先启动 `imserver`/`server` 再启动其客户端）、或 ZooKeeper（`project/test_zk_viper`）。写死的地址/端口以 `const` 常量块的形式位于各程序 `main.go` 顶部。

仓库中没有单元测试（`*_test.go`），也没有 lint 配置；这里所谓"跑测试"指的是运行某个演示程序。

## 第三方依赖

这些依赖需预先存在于 GOPATH 上（无 vendor 目录，无 lock 文件）：
- `github.com/gomodule/redigo/redis` —— Redis 客户端（`storage/cache` 连接池）
- `github.com/spf13/viper`、`github.com/fsnotify/fsnotify` —— 配置
- `github.com/samuel/go-zookeeper/zk` —— ZooKeeper 配置中心
- `github.com/thinkboy/log4go`、`github.com/Terry-Mao/goconf` —— 日志/配置

## 值得了解的共享代码

- `common/signal.go` —— `common.InitSignal()` 阻塞等待 SIGHUP/SIGQUIT/SIGTERM/SIGINT 信号；程序调用它以保持运行直到被 kill。
- `storage/cache/redis.go` —— 共享的 redigo 连接池（`GetRedisConn`），以 `go-test/storage/cache` 导入。

这两个是仅有的跨程序边界被引用的包；其余目录均各自独立（注意 `redis/pressure`、`redis/redis_list/producer`、`test/tcp/imclient` 各自携带**自己的** `proto.go`/`redis.go` 副本，而非共享）。

## 部署到远程主机

`rsync.sh` 仅将 `~/go/src/go-test` 下的 `*.go` 文件推送到远程主机的 GOPATH（`root@lu20:/root/go/src`）——预期工作流是：本地编辑，rsync 到 Linux 机器，在那里构建/运行。
