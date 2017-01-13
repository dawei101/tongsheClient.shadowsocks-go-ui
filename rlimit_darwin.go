package main

import (
	"log"
	"syscall"
)

func setrlimit() {
	var rl syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rl)
	if err != nil {
		log.Printf("get rlimit error: %v", err)
	}
	rl.Cur = 99999
	rl.Max = 99999
	err = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rl)
	if err != nil {
		log.Printf("set rlimit error: %v", err)
	}
}
