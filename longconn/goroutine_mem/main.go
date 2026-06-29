package main

import (
	"flag"
	"fmt"
	"runtime"
	"sync"
)

// 测试目标：测量 N 个阻塞的 goroutine 实际占用多少内存，
// 用来佐证「百万连接 → 两百万 goroutine 内存吃不消」这个论点。

var (
	num   = flag.Int("n", 1000000, "要创建的 goroutine 数量")
	depth = flag.Int("depth", 0, "阻塞前递归的函数调用深度，用来模拟更深的调用栈")
)

// block 先递归 d 层，再在最深处阻塞，用来模拟真实业务里
// goroutine 因为调用层级变深而触发的 stack growth。
func block(d int, ch chan struct{}, wg *sync.WaitGroup) {
	if d > 0 {
		// 放一个本地数组，避免编译器把递归优化掉，同时占用一点栈空间
		var pad [64]byte
		_ = pad
		block(d-1, ch, wg)
		return
	}
	wg.Done()
	<-ch // 永久阻塞，模拟连接 goroutine 卡在 Read 上
}

func readMem() runtime.MemStats {
	var m runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m)
	return m
}

func main() {
	flag.Parse()

	before := readMem()

	ch := make(chan struct{}) // 永不关闭，让所有 goroutine 一直 park 住
	var wg sync.WaitGroup
	wg.Add(*num)
	for i := 0; i < *num; i++ {
		go block(*depth, ch, &wg)
	}
	wg.Wait() // 等所有 goroutine 都进入阻塞状态

	after := readMem()

	mb := func(b uint64) float64 { return float64(b) / 1024 / 1024 }
	perG := func(b uint64) float64 { return float64(b) / float64(*num) }

	fmt.Printf("goroutine 数量      : %d\n", *num)
	fmt.Printf("递归调用深度        : %d\n", *depth)
	fmt.Printf("当前存活 goroutine  : %d\n\n", runtime.NumGoroutine())

	dStack := after.StackInuse - before.StackInuse
	dHeap := after.HeapAlloc - before.HeapAlloc
	dSys := after.Sys - before.Sys

	fmt.Printf("%-22s %10.2f MB  (每协程 %6.0f 字节)\n", "栈内存 StackInuse 增量", mb(dStack), perG(dStack))
	fmt.Printf("%-22s %10.2f MB  (每协程 %6.0f 字节)\n", "堆内存 HeapAlloc 增量", mb(dHeap), perG(dHeap))
	fmt.Printf("%-22s %10.2f MB  (每协程 %6.0f 字节)\n", "进程总内存 Sys 增量  ", mb(dSys), perG(dSys))
	fmt.Printf("\n合计 栈+堆 每协程    : %.0f 字节 (%.2f KB)\n",
		perG(dStack)+perG(dHeap), (perG(dStack)+perG(dHeap))/1024)
}
