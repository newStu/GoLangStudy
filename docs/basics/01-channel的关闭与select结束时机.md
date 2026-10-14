# Go 笔记:channel 关闭与 select 的结束时机

> 场景:多个协程向带缓冲的 channel 发数据,主协程用 `select` 交替消费两个 channel,全部消费完后汇总。
> 这篇笔记记录我踩过的坑:**用一条额外的 `chanDone` 当结束信号,并且和数据 channel 同时关闭,为什么会错**。
>
> 配套代码:
> - `Demo/goroutine/index.go` —— 出问题的写法(`go run ./Demo/goroutine/index.go`,注意要单文件运行,该文件夹里还有一个 `协程超时.go` 也声明了 `main`,整包 `go run ./Demo/goroutine` 会报 main 重定义);
> - `Demo/channel` —— 第五节修复版(`go run ./Demo/channel`);
> - `Demo/timeout` —— 第七节超时演示,3 秒超时、2 秒完成,走"执行完毕"分支(`go run ./Demo/timeout`);1 秒就超时的变体在 `Demo/goroutine/协程超时.go`(单文件运行)。

## 一、出问题的写法

```go
var chanGroup    = make(chan int, 3)
var chanNameGroup = make(chan string, 3)
var chanDone     = make(chan struct{})

func shopping(name string, money int, wait *sync.WaitGroup) {
	defer wait.Done()
	// ... 购物 ...
	chanGroup <- money
	chanNameGroup <- name
}

func main() {
	var wait sync.WaitGroup
	wait.Add(3)
	go shopping("Alice", 100, &wait)
	go shopping("Bob", 200, &wait)
	go shopping("Tom", 200, &wait)

	// 等 3 个人都完成后,关闭所有 channel
	go func() {
		wait.Wait()
		close(chanGroup)
		close(chanNameGroup)
		close(chanDone) // ← 问题所在:和数据 channel 同时关闭
	}()

	for {
		select {
		case name := <-chanNameGroup:
			nameList = append(nameList, name)
		case money := <-chanGroup:
			moneyList = append(moneyList, money)
			total += money
		case <-chanDone:
			return // ← 想在这里结束
		}
	}
}
```

期望:3 个人共 500 元,`nameList` 3 个名字、`moneyList` 3 笔金额。
实际(连跑 10 次,结果每次都不一样):

```
总共花费 500 元   金额列表: [100 200 200]
总共花费 300 元   金额列表: [200 100 200 0]
总共花费 500 元   金额列表: [100 200 200 0 0 0 0]   ← 多出零值
总共花费 100 元   金额列表: [100]                    ← 丢了数据
总共花费 400 元   金额列表: [200 200]
...
```

## 二、三个必须记住的 channel 语义

分析之前,先记住三条规则,所有的坑都源于它们:

1. **`select` 在多个就绪的 case 中随机选一个**。没有任何优先级,"先来的先选"不成立。
2. **从已关闭的 channel 接收不会阻塞**:缓冲区有数据先给数据,给完了立刻返回零值(`int` → `0`,`string` → `""`),永远"就绪"。
3. **从 nil channel 接收会永远阻塞**,所以在 select 里永远不就绪 —— 这条是后面修复技巧的依据。

## 三、同时关闭的弊端:两个方向的错误

`wait.Wait()` 返回只能证明**数据生产完了**(发送在 `defer wait.Done()` 之前),不能证明**数据消费完了**。把 `chanDone` 和数据 channel 在同一时刻关闭,`select` 的三个 case 从这一刻起**全部永久就绪**,再叠加规则 1(随机选择),就产生两种相反的错误:

### 错误 1:提前退出,丢数据

`chanGroup`/`chanNameGroup` 的缓冲区里可能还有没消费的值,但某一轮 `select` 随机选中了 `<-chanDone` 直接 `return` —— 剩下的数据全被扔掉。
上面"总共 100 元 / 400 元"就是这么来的。**丢多少、丢不丢,全凭随机**,这种 bug 复现起来时有时无,最难查。

### 错误 2:反复读到零值,收垃圾数据

反过来,某一轮 `select` 随机选中已关闭的 `chanGroup`,立刻收到零值 `0`,还被 append 进列表、加进总额。
上面金额列表里多出来的 `0 0 0 0` 就是这么来的;`chanNameGroup` 同理会读到空字符串 `""`。

**根源是同一个:把"生产完毕的信号"误当成了"消费完毕的信号"。** `close(chanDone)` 的那一刻,消费端可能还没开始读第 2 条数据 —— 关闭时机和消费进度之间没有任何同步关系。

## 四、"加个延时再关闭"为什么也不行

第一反应的修法:数据 channel 关掉之后 `sleep` 一秒再关 `chanDone`,给消费端留时间:

```go
go func() {
	wait.Wait()
	close(chanGroup)
	close(chanNameGroup)
	time.Sleep(time.Second) // ← 想等消费端读完
	close(chanDone)
}()
```

真数据确实不丢了,但结果更糟:**单次运行输出 12.8MB** —— `moneyList` 里被塞进几十万个 `0`,`nameList` 里塞满 `""`。

原因:这 1 秒里 `chanDone` 还阻塞,select 只有两个数据 channel 就绪;而它们已关闭,**接收永不阻塞**(规则 2),真数据读完后,循环变成全速空转,一轮一轮读零值、append,直到 `chanDone` 关闭才有机会退出。总金额碰巧还对(加 0 不影响),但列表彻底废了,还白烧 1 秒 CPU。

更深一层:**"sleep 一段时间等另一边处理完"是并发的经典反模式** —— 它靠猜时间换正确性,永远无法证明"睡够了"。这次碰巧不出错,只是因为关闭后的 channel 读起来不阻塞;换一个消费端真正需要时间的场景,sleep 多短都可能是错的。**正确性应该由同步机制(channel 语义)本身保证,而不是由时钟保证。**

## 五、正确做法:关闭本身就是结束信号

Go 里标准做法是**不要额外的 done channel**,让"数据 channel 关闭且排空"直接当结束信号,接收端用 `v, ok := <-ch` 区分真数据和关闭:

```go
// 等 3 个人都完成后关闭数据 channel(不需要 chanDone 了)
go func() {
	wait.Wait()
	close(chanGroup)
	close(chanNameGroup)
}()

nameCh := chanNameGroup
moneyCh := chanGroup
for nameCh != nil || moneyCh != nil {
	select {
	case name, ok := <-nameCh:
		if !ok {
			nameCh = nil // 已关闭且排空,置 nil 禁用这个 case
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
```

要点:

- `ok == false` 表示该 channel **已关闭且缓冲区已排空** —— 缓冲的值会先被读出,所以一条真数据不丢,一个零值不多收;
- 把关闭的 channel **置为 `nil`**,从 nil channel 接收永远阻塞(规则 3),该 case 在 select 里自动失效,没有空转;
- 循环条件 `nameCh != nil || moneyCh != nil`:两个都耗尽才退出,**退出时刻精确等于"最后一条真数据被消费"的时刻**,由 channel 语义保证,不依赖任何时序运气;
- 用局部变量 `nameCh`/`moneyCh` 而不是直接置 nil 全局 channel,不污染全局状态。

验证:连跑 15 次全部 `总共花费 500 元`,列表始终 3 个名字、3 笔金额。

## 六、同场景的简化写法(对比)

select + nil 是"不知道数据何时结束、又要交替消费多个 channel"时的通用解法,本场景其实有更简单的选择:

```go
// 写法 A:已知人数,最直白。
// 注意:<-ch 在没数据时会阻塞等待,所以循环的节奏由数据到达驱动,
// 不是"跑得快取不到" —— 第 1 轮会停在那等第一个人购物结束。
// 前提:循环次数必须精确等于发送总数,多了会永久阻塞(死锁),
// 少了会剩数据没人收。
for i := 0; i < 3; i++ {
	name := <-chanNameGroup
	money := <-chanGroup
	nameList = append(nameList, name)
	total += money
}

// 写法 B:一条 struct channel,数据天然绑定,连关闭判断都省了
type person struct { name string; money int }
ch := make(chan person, 3)
// 发送端:ch <- person{"Alice", 100}
for p := range ch { // channel 关闭且排空后 for range 自动退出
	total += p.money
}
```

**注意:写法 A 并不保证 `nameList[i]` 和 `moneyList[i]` 是同一个人。** 两个发送者向两条 channel 发送的相对顺序可以交错:比如 Alice 发完金额后被暂停,Bob 连发了金额和名字,Alice 才发名字 —— 那么 `chanGroup` 里是 `[100, 200]`,`chanNameGroup` 里却是 `["Bob", "Alice"]`,第 1 轮就会把 "Bob" 和 100 配对。想让名字和金额严格绑定,只有写法 B(一条 struct channel)能做到 —— 这也是两条独立 channel 传相关数据的固有缺陷。

## 七、协程超时:select + time.After

前面的写法都有一个共同前提:**每个协程最终都会完成任务(或至少汇报失败)**。如果某个协程永久卡住 —— 比如挂在一个没有超时的网络读上 —— 接收端就会无限等待。这时需要一根"保险丝":超时。

```go
var done = make(chan struct{})

func event() {
	fmt.Println("Event 开始执行")
	time.Sleep(2 * time.Second) // 模拟耗时操作
	fmt.Println("Event 执行结束")
	close(done) // 完成时关闭 done,作为结束信号
}

func main() {
	go event()
	select {
	case <-time.After(3 * time.Second): // 保险丝:最多等 3 秒
		fmt.Println("超时")
	case <-done: // 正常完成
		fmt.Println("Event 执行完毕")
	}
}
```

这正是 select "多路就绪、随机选择"特性的正确用法:两个 case 分别等"完成信号"和"定时器",谁先就绪走谁。实测两个分支:

- `time.Sleep(2秒)` vs `time.After(3秒)` → `Event 执行完毕`(done 先就绪);
- 把超时改成 `time.After(1秒)` → `超时`(定时器先就绪)。

这里 `done` 用 `close` 而不是 `done <- struct{}{}` 有个好处:close 是广播,任意多个等待者都能收到,而且不需要发送方还活着。

### 两个容易忽略的细节

1. **超时不会"杀掉"协程**。打印"超时"后 main 直接返回,进程退出,`event` 协程被连带终止 —— 但注意看超时那次运行的输出里没有"Event 执行结束":Go 没有任何手段强制杀死一个协程,超时只是**接收端不再等了**,协程本身如果没人退出它,它就会继续跑(占着资源)直到进程结束。想让协程配合退出,要靠 `context` 取消信号在协程内部主动 return。
2. **`time.After` 在没触发时不会立刻回收**。走到 `done` 分支时,那个 3 秒的定时器仍然会活到到期才释放。一次性场景无所谓,但放在高频循环里会累积,应该用 `t := time.NewTimer(d)` + `t.Stop()`。更省心的做法是直接用 `context.WithTimeout`:

```go
ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
defer cancel()
select {
case <-ctx.Done():
	fmt.Println("超时:", ctx.Err()) // context deadline exceeded
case <-done:
	fmt.Println("Event 执行完毕")
}
```

`context` 版还能把取消信号传递给协程内部(`event` 里 `select { case <-ctx.Done(): return }`),让"超时"真正变成"放弃等待 + 通知对方退出",这是生产代码的标准形态。

## 八、和前端异步的对照(有 JS 背景时快速锚定)

| 前端(JS) | Go | 对应关系 |
|---|---|---|
| `Promise` resolve | 发送 + `close` | 一次性的完成通知,多个消费者都能收到 |
| `await` | `<-ch` | 挂起当前执行流,等到结果再继续 |
| `Promise.all` | `sync.WaitGroup` + `close` | 等所有任务完成 |
| `Promise.race` | `select` | 谁先就绪走谁(超时案例 = `Promise.race([task, timeout])`) |
| `setTimeout` | `time.After` | 定时器 |
| `AbortController` | `context` | 协作式取消,两边都杀不掉对方,只能请求退出 |

两条最需要翻转的直觉:

1. **真并行 vs 单线程**:JS 并发只是事件循环上的交错,任何时刻只有一段代码在跑,数据竞争在语言层面不可能;goroutine 真的同时跑在多个核上,所以 channel 一半的意义是**同步**,不只是传数据。
2. **阻塞的含义相反**:JS 里阻塞 = 卡死整个线程,是禁忌;Go 里 `<-ch` 只挂起当前 goroutine,调度器去跑别的 —— 阻塞不昂贵,所以 Go 敢写成"看起来同步"的代码。可以记成:**goroutine + channel = 把 JS 里"被迫异步"的代码还原本成同步的样子**。

另外 Promise 是一次性的(resolve 一次、一个值、无背压),channel 是可持续的队列(发 N 条、多生产多消费、无缓冲版自带背压)—— "关闭后读到零值"的坑是 channel 作为"可耗尽队列"特有的,Promise 没有对应物;JS 流(`ReadableStream`)读完后 `read()` 返回 `{done: true}`,则能看到 `ok == false` 的影子。

## 九、一句话总结

| 写法 | 结果 |
|---|---|
| `chanDone` 与数据 channel 同时关闭 | 随机丢数据 / 随机收零值,时好时坏 |
| sleep 之后再关 `chanDone` | 不丢真数据,但空转 + 收满零值,靠时间猜正确性 |
| `ok` 判断 + 置 `nil` | 精确结束,不多不少 |
| 协程可能卡死时只等 channel | 接收端永久阻塞,无报错 |
| select + `time.After` / `context` 超时 | 加保险丝,卡死也能自救 |

**核心认知:`close(ch)` 只代表"生产完毕",不代表"消费完毕"。** 想知道消费完毕没有,唯一的办法是让接收端自己确认 —— `ok == false` 就是 channel 给你的那个确认;而"生产方永远不会完毕"的情况,要靠超时兜底。
