package main

import (
	"fmt"
	"time"
)

var done = make(chan struct{})

func event() {
	fmt.Println("Event 开始执行")
	time.Sleep(2 * time.Second)
	fmt.Println("Event 执行结束")
	close(done)
}

func main() {
	go event()
	select {
	case <-time.After(1 * time.Second):
		fmt.Println("超时")
	case <-done:
		fmt.Println("Event 执行完毕")
	}
}
