package main

import (
	"fmt"
	"go-test/common"
	"os"
)

// go run *.go map

func parseArgs() (string, bool) {
	args := os.Args
	if args == nil || len(args) <= 1 {
		fmt.Println("pls input test module")
		return "", false
	}
	module := args[1]
	fmt.Printf("test module: %v\n====================\n", module)
	return module, true
}

func main() {
	module, ok := parseArgs()
	if !ok {
		return
	}

	switch module {
	case "map":
		go testMapLogic()
	case "interface":
		go testInterfaceLogic()
	case "go":
		common.InitSignal()
	case "other":
		go testOther()
	case "chanel":
		go testChanelLogic()
	case "slice":
		go testSliceLoic()
	default:
		fmt.Println("not have test module:", module)
	}

	common.InitSignal()
}
