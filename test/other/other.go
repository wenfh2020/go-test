package main

import (
	"fmt"
)

// FIEO
func testRecover() {
	fmt.Println("*testRecover...")
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("")
			fmt.Printf("test recover hit. %v\n", r)
		}
	}()

	defer func() {
		fmt.Println("end.....")
	}()

	var arr []int
	arr[1] = 0
	//panic("test panic")
}

func testDefer() int {
	fmt.Println("*testDefer...")
	i := 10
	fmt.Println("test value:", i)
	go func() {
		i++
		fmt.Println("go func:", i)
	}()

	defer func() {
		i++
		fmt.Println("defer:", i)
	}()

	return i
}

func testOther() {
	fmt.Println("*testOther...")
	/*
		testArray()

			// slice := [make([]int, 10, 30)]
			slice := []int{1}
			s2 := slice
			fmt.Printf("%p, %p\n", &slice, &s2)

			changeSlice(slice)
	*/
	// fmt.Printf("before,slice %v, addr is %p \n", slice, &slice)
	// changeSlice(&slice)
	// fmt.Printf("after,slice %v, addr is %p \n", slice, &slice)
	/*
		testRecover()
		fmt.Println("main:", testDefer())
		time.Sleep(1 * time.Second)
	*/
	// var arr [10]int
	// testArray()
}

func main() {
	testOther()
}
