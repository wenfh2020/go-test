package main

import (
	"fmt"
)

/*
数组形参是数据拷贝
slice, map 形参传递是对象拷贝，但是新参数对象与实参对象会指向同一个数据。
但是，形参在函数内部可能会发生内存变动，形参和实参同步的话，参数最好填指针。
*/

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
