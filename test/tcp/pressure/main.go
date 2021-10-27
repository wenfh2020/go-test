/* ./main --cnt 1000 */

package main

import (
	"flag"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	timeFmt = "2006-01-02 15:04:05.000"
	connCnt = 1000
)

var cnt int
var failed int32
var wait sync.WaitGroup

func main() {
	flag.IntVar(&cnt, "cnt", connCnt, "connect count")
	flag.Parse()

	wait.Add(cnt)

	begin := time.Now()
	// fmt.Println("---\nbegin time:", begin.Format(timeFmt))

	for i := 0; i < cnt; i++ {
		go func(index int) {
			addr, _ := net.ResolveTCPAddr("tcp", "127.0.0.1:80")
			c, err := net.DialTCP("tcp", nil, addr)
			if err != nil {
				atomic.AddInt32(&failed, 1)
				fmt.Printf("%d, connect failed!, err: %v\n", index, err)
			} else {
				// fmt.Printf("%d, connect ok!\n", index)
				defer c.Close()
			}
			wait.Done()
		}(i)
	}

	wait.Wait()

	// end := time.Now().Format(timeFmt)
	spend := time.Now().Sub(begin).Seconds()
	// fmt.Println("end time:", end)
	fmt.Printf("cnt: %d, failed: %d, spend: %v\n", cnt, failed, spend)
}
