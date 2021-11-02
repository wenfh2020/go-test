/* https://stackoverflow.com/questions/24455147/how-do-i-send-a-json-string-in-a-post-request-in-go */

package main

import (
	"bytes"
	"flag"
	"fmt"
	// "io/ioutil"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

const (
	timeFmt = "2006-01-02 15:04:05.000"
	connCnt = 10
	// host    = "xxxx.com"
	host = "172.16.230.15"
)

var cnt int
var failed int32
var wait sync.WaitGroup

func main() {
	flag.IntVar(&cnt, "cnt", connCnt, "connect count")
	flag.Parse()

	wait.Add(cnt)
	begin := time.Now()
	url := fmt.Sprintf("http://%s/hello", host)

	for i := 0; i < cnt; i++ {
		go func(index int) {
			jsonStr := []byte(`{"title":"Buy cheese and bread for breakfast."}`)
			req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonStr))
			req.Header.Set("Content-Type", "application/json; charset=UTF-8")

			client := &http.Client{}
			res, err := client.Do(req)
			if err != nil {
				atomic.AddInt32(&failed, 1)
				fmt.Printf("%d, connect failed!, err: %v\n", index, err)
			} else {
				defer res.Body.Close()
			}

			/* response. */
			// fmt.Println("response Status:", res.Status)
			// fmt.Println("response Headers:", res.Header)
			// bytes, _ := ioutil.ReadAll(res.Body)
			// fmt.Println("response Body:", string(bytes))
			wait.Done()
		}(i)
	}

	wait.Wait()
	spend := time.Now().Sub(begin).Seconds()
	fmt.Printf("cnt: %d, failed: %d, spend: %v\n", cnt, failed, spend)
}
