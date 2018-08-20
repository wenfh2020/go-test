package main

import (
	"go-test/common"
	"go-test/storage/cache"
)

const (
	REDIS_ADDR = "127.0.0.1:26379" //redis addr
)

func main() {
	cache.InitRedis(REDIS_ADDR)
	//go SentinelGetMasterAddr("mymaster")
	go SentinelGetSlavesAddr("mymaster")
	common.InitSignal()
}
