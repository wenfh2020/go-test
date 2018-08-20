package main

import (
	"go-test/common"
	"time"

	"github.com/gomodule/redigo/redis"
	log "github.com/thinkboy/log4go"
)

const (
	MASTER_NAME     = "mymaster"
	SENTINEL_ADDR_1 = "127.0.0.1:26379"
	SENTINEL_ADDR_2 = "127.0.0.1:26380"
)

func InitSentinel(arrHost []string, strMasterName string) (p *Sentinel, err error) {
	p = &Sentinel{
		Addrs:      arrHost,
		MasterName: strMasterName,
		Dial: func(strAddr string) (redis.Conn, error) {
			iTime := 500 * time.Millisecond
			c, err := redis.DialTimeout("tcp", strAddr, iTime, iTime, iTime)
			if err != nil {
				return nil, err

			}
			return c, nil
		},
	}
	return p, nil
}

func main() {
	arrHost := []string{SENTINEL_ADDR_1, SENTINEL_ADDR_2}
	pSentinel, err := InitSentinel(arrHost, MASTER_NAME)
	if err != nil {
		log.Error(err)
		return
	}

	go func() {
		for {
			time.Sleep(3 * time.Second)

			pSentinel.Discover()
			log.Info("--\nsentinels: %v", pSentinel.Addrs)

			strMasterAddr, err := pSentinel.MasterAddr()
			if err != nil {
				log.Error(err)
				continue
			}

			log.Info("master addr: %s", strMasterAddr)

			arrSlaves, err := pSentinel.Slaves()
			if err != nil {
				log.Error(err)
				continue
			}

			for i, v := range arrSlaves {
				log.Info("slaves: %d, %v", i, v)
			}
		}
	}()

	common.InitSignal()
}
