package main

import (
	"fmt"
	"go-test/common"
	"os"
)

// go run *.go map

func main() {
	args := os.Args
	if args == nil || len(args) <= 1 {
		fmt.Println("pls input test module")
		return
	}
	module := args[1]

	switch module {
	case "map":
		testMapLogic()
	case "interface":
		testInterfaceLogic()
	case "go":
		common.InitSignal()
	default:
		fmt.Println("not have test module:", module)
	}
}
