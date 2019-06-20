package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/viper"
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

func watch(v chan *viper.Viper) {
	for {
		select {
		case config := <-v:
			{
				log := config.GetString("file")
				fmt.Println(log)
			}
		}
		time.Sleep(time.Second * 3)
	}
}

func main() {
	InitConfigCenter()
	config, err := GetModule("/wallet/wallet.yml", "")
	if err != nil {
		panic(err)
	}
	fmt.Println(config.GetInt("req_trade_no_cache_expired_sec"))

	// confcenter.Test()

	// fmt.Println("-----------------------")
	// p, err := filepath.Abs("./conf")
	// fmt.Println(p, err)
	// fmt.Println(err)
	// fmt.Println(config.GetInt("req_trade_no_cache_expired_sec"))
	// viper.SetConfigFile("./conf/app.yml")
	// err := viper.ReadInConfig()
	// if err != nil {
	// 	fmt.Println(err)
	// 	return
	// }

	// c := make(chan *viper.Viper, 1)
	// defer func() {
	// 	close(c)
	// }()
	// viper.WatchConfig()
	// viper.OnConfigChange(func(e fsnotify.Event) {
	// 	fmt.Println("file change..", e.Name)
	// 	c <- viper.Sub("log")
	// })

	// encoding := viper.GetString("log.encoding")
	// fmt.Println(encoding)
	// go watch(c)
	initSignal() 
}
