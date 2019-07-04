package main

import "fmt"

/*
数组形参是数据拷贝
slice, map 形参传递是对象拷贝，但是新参数对象与实参对象会指向同一个数据。
但是，形参在函数内部可能会发生内存变动，形参和实参同步的话，参数最好填指针。
*/

// 形参对实参的内容进行拷贝。（深拷贝）
func testArrayFunc(p *[10]int) {
	fmt.Println("*testArrayFunc...")
	fmt.Printf("p: %v, addr: %p\n", *p, p)
	p[0] = 1111
	fmt.Printf("p: %v, addr: %p\n", *p, p)
}

func testArray() {
	fmt.Println("*testArray...")
	arr := [10]int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	fmt.Printf("1 array: %v, addr: %p\n", arr, &arr)

	testArrayFunc(&arr)
	fmt.Printf("2 array: %v, addr: %p\n", arr, &arr)
}

// 形参拷贝实参的数据结构后，形式惨还是指向相同的数据（浅拷贝），append 增加的内存数据，形参的 len 和 cap 会发生变化，但是实参是不变的。
func testSliceFunc(s []int) {
	fmt.Println("*testSliceFunc...")
	fmt.Printf("1 test slice: %v, ptr: %p, len: %d, cap: %d\n", s, &s, len(s), cap(s))
	s[0] = 2222
	fmt.Printf("2 test slice: %v, ptr: %p, len: %d, cap: %d\n", s, &s, len(s), cap(s))
	s[1] = 333
	fmt.Printf("3 test slice: %v, ptr: %p, len: %d, cap: %d\n", s, &s, len(s), cap(s))
	s = append(s, 44)
	fmt.Printf("4 test slice: %v, ptr: %p, len: %d, cap: %d\n", s, &s, len(s), cap(s))
}

func testSlice() {
	fmt.Println("*testSlice...")
	s := make([]int, 2, 2)
	s[0] = 1
	fmt.Printf("1 slice: %v, ptr: %p, len: %d, cap: %d\n", s, &s, len(s), cap(s))
	testSliceFunc(s)
	fmt.Printf("2 slice: %v, ptr: %p, len: %d, cap: %d\n", s, &s, len(s), cap(s))
}

func testSliceLogic() {
	fmt.Println("*testOther...")

	// testArray()
	testSlice()
}
