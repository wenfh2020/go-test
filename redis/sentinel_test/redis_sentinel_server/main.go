package main

import (
	"go-test/common"
	"time"

	"github.com/gomodule/redigo/redis"
	log "github.com/thinkboy/log4go"
)

const (
	REDIS_ADDR = "127.0.0.1:26379" //redis addr
)

func InitSentinel(arrHost []string, strMasterName string) (p *Sentinel, err error) {
	p = &Sentinel{
		Addrs:      arrHost,
		MasterName: strMasterName,
		Dial: func(strAddr string) (redis.Conn, error) {
			timeout := 500 * time.Millisecond
			c, err := redis.DialTimeout("tcp", strAddr, timeout, timeout, timeout)
			if err != nil {
				return nil, err

			}
			return c, nil

		},
	}
	return p, nil
}

func main() {
	go func() {
		strMasterName := "mymaster"
		arrHost := []string{"127.0.0.1:26380", "127.0.0.1:26379"}
		pSentinel, err := InitSentinel(arrHost, strMasterName)
		if err != nil {
			log.Error(err)
			return
		}

		for {
			time.Sleep(3 * time.Second)

			pSentinel.Discover()
			log.Info("sentinels: %v", pSentinel.Addrs)

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
