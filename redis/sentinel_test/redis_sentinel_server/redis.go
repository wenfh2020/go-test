package main

import (
	"time"

	"./sentinel"
	"github.com/gomodule/redigo/redis"
)

const (
	IDLE_COUNT   = 2   //连接池空闲个数
	ACTIVE_COUNT = 2   //连接池活动个数
	IDLE_TIMEOUT = 180 //空闲超时时间
)

var (
	REDIS_DB     int
	RedisClients *redis.Pool
)

func GetRedisConn() (conn redis.Conn) {
	return RedisClients.Get()
}

func InitRedis(strHost string) {
	REDIS_DB = 0
	RedisClients = createPool(IDLE_COUNT, ACTIVE_COUNT, IDLE_TIMEOUT, strHost)
}

func InitSentinel(arrHost []string, strMasterName string) (err error, p *sentinel.Sentinel) {
	p = &sentinel.Sentinel{
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
	return nil, p
}

func createPool(iMaxIdle, iMaxActive, iIdleTimeout int, strAddr string) (pool *redis.Pool) {
	pool = new(redis.Pool)
	pool.MaxIdle = iMaxIdle
	pool.MaxActive = iMaxActive
	pool.Wait = true
	pool.IdleTimeout = (time.Duration)(iIdleTimeout) * time.Second
	pool.Dial = func() (redis.Conn, error) {
		c, err := redis.Dial("tcp", strAddr)
		if err != nil {
			return nil, err
		}
		c.Do("SELECT", REDIS_DB)
		return c, err
	}
	return
}
