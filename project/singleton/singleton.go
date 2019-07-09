package main

import (
	"fmt"
	"sync"
	"time"
)

//instance test

// Singleton obj
type Singleton struct {
	lock *sync.RWMutex
	data string
}

var instance *Singleton
var once sync.Once

func setupInstance() {
	if instance == nil {
		instance = &Singleton{
			lock: &sync.RWMutex{},
			data: "init",
		}

		fmt.Println("instance set up....")
	}
}

// GetInstance get singleton instance
func GetInstance() *Singleton {
	once.Do(setupInstance)
	return instance
}

func (c *Singleton) setdata(s string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.data = s
	fmt.Println("set data:", instance.data)
}

func (c *Singleton) getdata() string {
	c.lock.RLock()
	defer c.lock.RUnlock()
	fmt.Println("get data:", c.data)
	return c.data
}

func testGetData() {
	GetInstance().getdata()
	time.Sleep(100 * time.Millisecond)
}

func testSetData(d string) {
	GetInstance().setdata(d)
	time.Sleep(100 * time.Millisecond)
}

func test() {
	for i := 0; i < 50; i++ {
		go testSetData(fmt.Sprintf("data:%d", i))
		go testGetData()
	}

	for i := 0; i < 20; i++ {
		go testGetData()
	}
}

func main() {
	test()
	time.Sleep(5 * time.Second)
}
