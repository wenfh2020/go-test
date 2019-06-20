package main

import (
	"fmt"
	"go-test/common"
)

func main() {
	InitConfigCenter()
	config, err := GetModule("/test/test.yml", "")
	if err != nil {
		panic(err)
	}
	fmt.Println(config.GetInt("test"))
	common.InitSignal()
}
