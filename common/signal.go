/*wenfahua/2018-8-15/signal handle*/

package common

import (
	"os"
	"os/signal"
	"syscall"
)

// InitSignal watch sys's signal
func InitSignal() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT)
	for {
		s := <-c
		switch s {
		case syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT:
			return
		case syscall.SIGHUP:
			return
		default:
			return
		}
	}
}
