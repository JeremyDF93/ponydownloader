package main

import (
	"os"
	"os/signal"
)

var (
	interrupter = make(chan struct{}, 1)
)

func init() {
	sig := make(chan os.Signal)
	signal.Notify(sig, os.Interrupt)

	go func() {
		<-sig
		close(interrupter)
		<-sig
		lDone("Program interrupted by user's command")
		os.Exit(0)
	}()
}

func interrupt(imgchan <-chan Image) (outch chan Image) {
	outch = make(chan Image)
	go func() {
		for {
			select {
			case <-interrupter:
				close(outch)
				return

			case img, ok := <-imgchan:
				if !ok {
					close(outch)
					imgchan = nil
					return
				}
				outch <- img
			}
		}
	}()

	return outch
}

func isInterrupted() bool {
	select {
	case <-interrupter:
		return true
	default:
		return false
	}
}
