package main

import (
	"fmt"
	"sync"
	"time"
)

var chanGroup = make(chan int, 3)
var chanNameGroup = make(chan string, 3)
var chanDone = make(chan struct{})

func shopping(name string, money int, wait *sync.WaitGroup) {
	defer wait.Done()
	fmt.Printf("%s 开始购物\n", name)
	time.Sleep(time.Second * 1)
	fmt.Printf("%s 购物结束，花费了 %d 元\n", name, money)

	chanGroup <- money
	chanNameGroup <- name
}

func main() {
	println("协程")
	var wait sync.WaitGroup
	wait.Add(3)
	go shopping("Alice", 100, &wait)
	go shopping("Bob", 200, &wait)
	go shopping("Tom", 200, &wait)

	// 单独的协程:等 3 个人都完成后关闭 channel,这样下面的 for range 才能结束
	go func() {
		wait.Wait()
		close(chanGroup)
		close(chanNameGroup)
		close(chanDone)
	}()

	total := 0
	var nameList []string
	var moneyList []int
	var event = func() {
		for {
			select {
			case name := <-chanNameGroup:
				nameList = append(nameList, name)
			case money := <-chanGroup:
				moneyList = append(moneyList, money)
				total += money
			case <-chanDone:
				return
			}
		}
	}
	event()
	fmt.Printf("所有人都购物完毕,总共花费 %d 元\n", total)
	fmt.Printf("姓名列表: %v\n", nameList)
	fmt.Printf("金额列表: %v\n", moneyList)
}
