package main

import (
	"errors"
	"time"

	"./sentinel"
	"github.com/gomodule/redigo/redis"
	log "github.com/thinkboy/log4go"
)

const (
	REDIS_ADDR = "127.0.0.1:26379" //redis addr
)

func newSentinelPool() *redis.Pool {
	sntnl := &sentinel.Sentinel{
		Addrs:      []string{":26379", ":26380", ":26381"},
		MasterName: "mymaster",
		Dial: func(addr string) (redis.Conn, error) {
			timeout := 500 * time.Millisecond
			c, err := redis.DialTimeout("tcp", addr, timeout, timeout, timeout)
			if err != nil {
				return nil, err

			}
			return c, nil

		},
	}
	return &redis.Pool{
		MaxIdle:     3,
		MaxActive:   64,
		Wait:        true,
		IdleTimeout: 240 * time.Second,
		Dial: func() (redis.Conn, error) {
			masterAddr, err := sntnl.MasterAddr()
			if err != nil {
				return nil, err

			}
			c, err := redis.Dial("tcp", masterAddr)
			if err != nil {
				return nil, err

			}
			return c, nil

		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			if !sentinel.TestRole(c, "master") {
				return errors.New("Role check failed")

			} else {
				return nil

			}

		},
	}

}

func main() {
	//InitRedis(REDIS_ADDR)
	//go SentinelGetMasterAddr("mymaster")
	//go SentinelGetSlavesAddr("mymaster")
	go func() {
		arrHost := []string{"127.0.0.1:26380", "127.0.0.1:26379"}
		err, pSentinel := InitSentinel(arrHost, "mymaster")
		if err != nil {
			log.Error(err)
			return
		}

		for {
			pSentinel.Discover()
			log.Info("%v", pSentinel.Addrs)

			strMasterAddr, err := pSentinel.MasterAddr()
			if err != nil {
				log.Error(err)
				time.Sleep(3 * time.Second)
				continue
			}

			log.Info("master addr = %s", strMasterAddr)

			arrSlaves, err := pSentinel.Slaves()
			if err != nil {
				log.Error(err)
				time.Sleep(3 * time.Second)
				continue
			}

			for i, v := range arrSlaves {
				log.Info("%d, %v", i, v)
			}

			time.Sleep(3 * time.Second)
		}
	}()

	InitSignal()
}
