package main

import (
	"fmt"
	"sync"
	"time"
)

// result 把"失败"也设计成数据的一部分,让故障能通过 channel 汇报
type result struct {
	name   string
	money  int
	failed bool
}

// shoppingBroken:违背发送约定的版本 —— 出问题时提前 return,什么都不发。
// 注意 defer wait.Done() 在第一行,所以 Done 一定会执行,Wait 不会被卡住。
func shoppingBroken(name string, money int, wait *sync.WaitGroup, ch chan result) {
	defer wait.Done()
	fmt.Printf("%s 开始购物\n", name)
	time.Sleep(500 * time.Millisecond)
	if name == "Tom" {
		fmt.Printf(">>> %s 购物出问题,提前返回,没有发送任何数据!\n", name)
		return
	}
	ch <- result{name: name, money: money}
}

// shoppingFixed:守约的版本 —— 失败也发一条带 failed 标记的记录
func shoppingFixed(name string, money int, wait *sync.WaitGroup, ch chan result) {
	defer wait.Done()
	fmt.Printf("%s 开始购物\n", name)
	time.Sleep(500 * time.Millisecond)
	if name == "Tom" {
		fmt.Printf(">>> %s 购物出问题,但把失败作为数据发回\n", name)
		ch <- result{name: name, failed: true}
		return
	}
	ch <- result{name: name, money: money}
}

func run(shopping func(string, int, *sync.WaitGroup, chan result), title string) {
	fmt.Printf("===== %s =====\n", title)
	ch := make(chan result, 3)
	var wait sync.WaitGroup
	wait.Add(3)
	go shopping("Alice", 100, &wait, ch)
	go shopping("Bob", 200, &wait, ch)
	go shopping("Tom", 200, &wait, ch)

	go func() {
		wait.Wait() // Tom 虽然没发数据,但 defer Done 执行了,Wait 正常返回
		close(ch)
	}()

	// 接收端:已知 3 个人,按人数收 3 条
	total, okCount := 0, 0
	for i := 0; i < 3; i++ {
		r := <-ch
		if r.failed {
			fmt.Printf("接收端: %s 购物失败(明确知道!)\n", r.name)
			continue
		}
		fmt.Printf("接收端: %s 花费 %d 元\n", r.name, r.money)
		total += r.money
		okCount++
	}
	fmt.Printf("结果: %d 人成功,总共 %d 元\n\n", okCount, total)
}

func main() {
	run(shoppingBroken, "场景一:协程出错时不发数据(违背约定)")
	run(shoppingFixed, "场景二:失败也作为数据从 channel 发回")
}
