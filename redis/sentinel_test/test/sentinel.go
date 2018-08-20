package main

import (
	"fmt"
	"go-test/storage/cache"

	"github.com/gomodule/redigo/redis"
	log "github.com/thinkboy/log4go"
)

func SentinelGetMasterAddr(strMasterName string) (err error, strHost string) {
	pConn := cache.GetRedisConn()
	if pConn.Err() != nil {
		fmt.Println(pConn.Err().Error())
		return
	}
	defer pConn.Close()

	log.Debug("sentinel get-master-addr-by-name %s", strMasterName)

	vals, err := redis.Values(pConn.Do("SENTINEL", "get-master-addr-by-name", strMasterName))
	if err != nil || len(vals) != 2 {
		fmt.Println(err.Error())
		return
	}

	strHost = fmt.Sprintf("%s:%s", string(vals[0].([]byte)), string(vals[1].([]byte)))
	log.Debug("host = %s", strHost)
	return
}

func SentinelGetSlavesAddr(strMasterName string) (err error) {
	pConn := cache.GetRedisConn()
	if pConn.Err() != nil {
		fmt.Println(pConn.Err().Error())
		return
	}
	defer pConn.Close()

	log.Debug("sentinel slaves %s", strMasterName)

	arrSlaves, err := queryForSlaves(pConn, strMasterName)
	if err != nil {
		log.Error("query slave failed! %v", err)
		return
	}

	log.Debug("slaves count = %d", len(arrSlaves))

	for i := 0; i < len(arrSlaves); i++ {
		log.Info("%d, %v", i, arrSlaves[i])
	}

	return
}

// Slave represents a Redis slave instance which is known by Sentinel.
type Slave struct {
	ip    string
	port  string
	flags string
}

func queryForSlaves(conn redis.Conn, masterName string) ([]*Slave, error) {
	res, err := redis.Values(conn.Do("SENTINEL", "slaves", masterName))
	if err != nil {
		return nil, err

	}
	slaves := make([]*Slave, 0)
	for _, a := range res {
		sm, err := redis.StringMap(a, err)
		if err != nil {
			return slaves, err

		}
		slave := &Slave{
			ip:    sm["ip"],
			port:  sm["port"],
			flags: sm["flags"],
		}
		slaves = append(slaves, slave)

	}
	return slaves, nil
}
