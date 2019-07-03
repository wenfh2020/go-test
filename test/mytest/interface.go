package main

import "fmt"

//Phone interface
type Phone interface {
	call() string
}

///////////

//IPhone strcut
type IPhone struct {
	name string
}

func (i *IPhone) call() string {
	i.name = "iphone call"
	return i.name
}

///////////

//XPhone strcut
type XPhone struct {
	name string
}

func (x *XPhone) call() string {
	x.name = "xphone call"
	return x.name

}

func testInterfaceLogic() {
	var p Phone

	p = new(IPhone)
	fmt.Println(p.call())

	p = new(XPhone)
	fmt.Println(p.call())
}

func main () {
	testInterfaceLogic()
}
