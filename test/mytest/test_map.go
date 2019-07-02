package main

import (
	"fmt"
	"sync"
	"time"
)

func testSafe() {
	fmt.Println("*testSafe...")
	var lock sync.RWMutex
	m := make(map[string]int)

	for i := 0; i < 100; i++ {
		go func(i int) {
			for j := 0; j < 100; j++ {
				lock.Lock()
				m[fmt.Sprintf("%d", i)] = j
				lock.Unlock()
			}
		}(i)
	}

	time.Sleep(20 * time.Second)
	fmt.Println(len(m), m)
}

func updateMap(m map[string]int) {
	fmt.Println("testMap", len(m))

	for i := 0; i < 200000; i++ {
		m[fmt.Sprintf("%d", i)] = i
	}

	fmt.Println("update len:", len(m))
	// m["33"] = 3
	// delete(m, "11")
}

func testUpdateMap() {
	fmt.Println("*testUpdateMap...")
	mapTest := map[string]int{"11": 1, "22": 2}
	fmt.Println("begin", mapTest)
	updateMap(mapTest)
	fmt.Println("end len:", len(mapTest))
	// fmt.Println("end", mapTest)
}

// TestMapLogic for map test exsample
func testMapLogic() {
	fmt.Println("*testMapLogic...")
	testUpdateMap()
	// testSafe()
}
