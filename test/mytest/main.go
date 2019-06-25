package main

import (
	"fmt"
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
	}
}
