package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func initSignal() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT, syscall.SIGSTOP)
	for {
		s := <-c
		switch s {
		case syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGSTOP, syscall.SIGINT:
			return
		default:
			return
		}
	}
}

func main() {
	InitConfigCenter()
	config, err := GetModule("/test/test.yml", "")
	if err != nil {
		panic(err)
	}
	fmt.Println(config.GetInt("test"))
	initSignal()
}
