//go:build ignore

package main

import (
	"fmt"
	"runtime"
)

// 测量不同类型/容量的 channel 平均占用多少堆内存。
// 做法：批量 make 出 N 个 channel 并保活，测 HeapAlloc 前后差值。

const N = 1_000_000

func measure(name string, makeOne func() interface{}) {
	keep := make([]interface{}, 0, N)
	runtime.GC()
	var b runtime.MemStats
	runtime.ReadMemStats(&b)

	for i := 0; i < N; i++ {
		keep = append(keep, makeOne())
	}

	runtime.GC()
	var a runtime.MemStats
	runtime.ReadMemStats(&a)

	// 减去 keep 切片本身每个元素 16 字节(interface{} 头)的开销
	per := float64(a.HeapAlloc-b.HeapAlloc)/float64(N) - 16
	fmt.Printf("%-28s 每个约 %5.0f 字节\n", name, per)

	runtime.KeepAlive(keep)
}

func main() {
	measure("make(chan struct{})  无缓冲", func() interface{} { return make(chan struct{}) })
	measure("make(chan struct{}, 8)", func() interface{} { return make(chan struct{}, 8) })
	measure("make(chan int)       无缓冲", func() interface{} { return make(chan int) })
	measure("make(chan int, 8)", func() interface{} { return make(chan int, 8) })
	measure("make(chan int, 64)", func() interface{} { return make(chan int, 64) })
	measure("make(chan int, 1024)", func() interface{} { return make(chan int, 1024) })
}
