package main

import (
	"fmt"
	"sync"
	"time"
)

/*
	https://segmentfault.com/a/1190000015036739?utm_source=tag-newest
	https://studygolang.com/articles/16774
*/

func testChanel(ch chan int) {
	fmt.Println("*testChanel...")
	i := <-ch
	fmt.Println("get", i)
}

func test(w, q chan int) {
	for {
		select {
		case i, ok := <-w:
			if ok {
				fmt.Println(i)
			} else {
				w = nil
				fmt.Println("quit w chanel")
			}
		case <-q:
			fmt.Println("quit")
			return
		}
	}
}

func testCloseChanel() {
	w := make(chan int)
	quit := make(chan int)
	go test(w, quit)
	go test(w, quit)

	w <- 1
	time.Sleep(10 * time.Second)

	close(w)
	time.Sleep(10 * time.Second)

	close(quit)
	time.Sleep(10 * time.Second)
}

func testTimeoutFunc() {
	fmt.Println("*testTimeoutFunc...")
	ch := make(chan int)

	go func() {
		fmt.Println("go timeout test func")
		time.Sleep(3 * time.Second)
		ch <- 1
	}()

	select {
	case r, ok := <-ch:
		if !ok {
			fmt.Println("get ch fail")
			return
		}
		fmt.Println("get test func ch", r)
	case <-time.After(2 * time.Second):
		fmt.Println("time out...")
		return
	}

	fmt.Println("testTimeoutFunc end...")
}

func testCacheChanel() {
	fmt.Println("*testCacheChanel...")
	ch := make(chan int)
	// go testChanel(ch)
	// ch <- 1
	// i := <-ch
	// fmt.Println(i)

	for i := 0; i < 10; i++ {
		select {
		case ch <- i:
			fmt.Println(i)
		case k := <-ch:
			fmt.Println(k)
			// default:
			// 	fmt.Println("default")
		}
	}

	fmt.Println("testCacheChanel end...")
}

var wait sync.WaitGroup

const (
	chSize   = 50000
	msgCount = 1000000
)

// 设置缓冲的队列，限制协程的增长速度
func testPressure() {
	fmt.Println("*testPressure...")

	chs := make(chan int, chSize)
	wait.Add(msgCount)

	fmt.Println("begin time:", oOldTime.Format(TIME_FOMAT))
	for i := 0; i < msgCount; i++ {
		chs <- 1
		go func(k int) {
			fmt.Println("test produce", k)
			time.Sleep(time.Millisecond)
			wait.Done()
			<-chs
		}(i)
	}

	wait.Wait()
	close(chs)
	fmt.Println("test pressure end...")
}

func testChanelLogic() {
	fmt.Println("*testChanelLogic...")

	// testTimeoutFunc()
	// testCloseChanel()
	// testCacheChanel()
	testPressure()
}

func main() {
	testChanelLogic()
}
