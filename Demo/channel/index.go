package main

import (
	"fmt"
	"sync"
	"time"
)

var chanGroup = make(chan int, 3)
var chanNameGroup = make(chan string, 3)

func shopping(name string, money int, wait *sync.WaitGroup) {
	defer wait.Done()
	fmt.Printf("%s 开始购物\n", name)
	time.Sleep(time.Second * 2)
	fmt.Printf("%s 购物结束，花费了 %d 元\n", name, money)

	chanGroup <- money
	chanNameGroup <- name
}

func main() {
	fmt.Println("协程")
	var wait sync.WaitGroup
	wait.Add(3)
	go shopping("Alice", 100, &wait)
	go shopping("Bob", 200, &wait)
	go shopping("Tom", 200, &wait)

	// 单独的协程:等 3 个人都完成后关闭 channel
	go func() {
		wait.Wait()
		close(chanGroup)
		close(chanNameGroup)
	}()

	total := 0
	var nameList []string
	var moneyList []int

	// select 版:消息一到就处理(交替消费两个 channel)
	// 技巧:channel 关闭且排空后(ok==false)把它置为 nil,
	// 从 nil channel 接收会永远阻塞,该 case 就不再就绪;
	// 两个都为 nil 时说明数据全部消费完,退出循环
	nameCh := chanNameGroup
	moneyCh := chanGroup
	for nameCh != nil || moneyCh != nil {
		select {
		case name, ok := <-nameCh:
			if !ok {
				nameCh = nil // 已关闭且排空,禁用这个 case
				continue
			}
			nameList = append(nameList, name)
		case money, ok := <-moneyCh:
			if !ok {
				moneyCh = nil
				continue
			}
			moneyList = append(moneyList, money)
			total += money
		}
	}

	fmt.Printf("所有人都购物完毕,总共花费 %d 元\n", total)
	fmt.Printf("姓名列表: %v\n", nameList)
	fmt.Printf("金额列表: %v\n", moneyList)
}
