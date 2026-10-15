# Go 笔记:审查 AI 生成的 Go 代码(实战三讲)

> 学 Go 的目标不是"会写"——AI 写得比人快。真正的目标是**会审**:AI 像一个手速极快、
> 从不提问、还特别自信的中级工程师,它能把功能实现出来,但**判断不了并发安不安全、
> 错误该不该吞、逻辑该不该存在**。这篇笔记把三轮"找 bug"实战整理成一套可复用的
> **审查肌肉记忆**,每一轮都藏了若干 AI 高频翻车的坑,外加一个"看着像 bug、其实安全"
> 的诱饵,专门训练"判断力"而不是"挑刺欲"。
>
> 阅读方式:先看题目代码,自己找一遍,再看解析。找到几个不重要,**能否说清"哪一行、
> 什么条件下炸"才是重点**。

## 一、先立观念:初级 vs 资深(senior)

判 bug 之前先有把尺子。初级和资深差的不是"会多少 API",而是**关注点**:

| 场景 | 初级 | 资深 |
|---|---|---|
| 看 `if err != nil { fmt.Println }` | "能跑,过" | "没 return,后面可能 nil 解引用" |
| 看 `return &u` | "看着怪,报个 bug" | "逃逸分析,安全,是诱饵,不提" |
| 拿到"加个缓存"需求 | 直接写个 map | 先问:并发吗?会大吗?失效策略? |
| 代码崩了 | 改掉崩溃那行 | 找根因,改共享函数,看别处是否也中招 |

> **初级解决"怎么实现";资深解决"要不要实现、会不会坏、坏了谁兜"。**

AI 现在满级的是"怎么实现"。所以你的位置不是跟它比谁写得多(必输),而是**坐到它上面**:
它输出,你审。**审得过它,你就是资深,它就是你的初级。**

## 二、三轮实战的公共敌人:三类 bug

三轮的坑看着五花八门,归到最后就三类,而且**恰好都是 AI 最爱犯、光看代码看不出来的**:

1. **要"脑内模拟运行时"才看得见** —— 竞态、死锁、泄漏。代码静态看没毛病,跑到特定时机才炸。
2. **要"想攻击者/想异常输入"才看得见** —— SQL 注入、未校验的参数、缺 return 导致的 panic。
3. **要"懂 Go 的语义边界"才看得见** —— zero value 溜进 JSON(nil slice → `null`)、
   `context` 传错、资源没关。

下面逐轮拆。

---

## 三、第一轮:并发拉取 + 缓存(4 坑 + 1 诱饵)

### 题目代码

```go
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

var cache = map[int]User{}

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func fetchUser(id int) *User {
	if u, ok := cache[id]; ok {
		return &u
	}

	resp, err := http.Get(fmt.Sprintf("https://api.example.com/users/%d", id))
	if err != nil {
		fmt.Println("fetch failed:", err)
	}

	var u User
	json.NewDecoder(resp.Body).Decode(&u)

	cache[id] = u
	return &u
}

func fetchAll(ids []int) []*User {
	var wg sync.WaitGroup
	results := make([]*User, 0, len(ids))

	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results = append(results, fetchUser(id))
		}()
	}

	wg.Wait()
	return results
}

func main() {
	users := fetchAll([]int{1, 2, 3})
	for _, u := range users {
		fmt.Println(u.ID, u.Name)
	}
}
```

### 坑 1:并发数据竞争(三处)

```go
if u, ok := cache[id]; ok { ... }   // 读 map
cache[id] = u                       // 写 map   ← 读+写并发
results = append(results, ...)      // 并发 append 同一个 slice
```

`fetchAll` 起多个 goroutine 并发跑 `fetchUser`,每个都读写全局 `cache`、都 append `results`。
**Go 的 map 和 slice 都不是并发安全的**:

- map 并发读写 → 运行时**主动检测**,直接 `fatal error: concurrent map read and map write`;
- slice 并发 append → 不一定崩,可能丢写/错乱,要靠 `go run -race` 才抓得到。

**注意**:不是"不能在 goroutine 里跑",是"**能跑,但结果不可信**"——这比直接崩溃更难查。

> 前端类比:JS 单线程,你从没担心过"两个函数同时改一个数组"。goroutine 是真并行 + 共享内存,
> 等于把 JS 换成多线程,那些以前不用想的竞态全回来了。

### 坑 2:错误被吞 → nil 解引用

```go
resp, err := http.Get(...)
if err != nil {
    fmt.Println("fetch failed:", err)   // 只打印,没 return
}
json.NewDecoder(resp.Body).Decode(&u)   // ← resp 是 nil,炸在这
```

`http.Get` 失败时 `resp` 是 **nil**,下一行 `resp.Body` 直接 **nil 解引用 panic**。
而且 `Decode` 的 err 也被无视。

> 前端类比:`fetch` 失败拿到 `undefined`,不打 `?.` 直接 `.data` → 炸。区别是 JS 会 `throw`
> 打断你;Go 是"print 一声当没事发生,愉快地继续跑",坑你更晚。

### 坑 3:资源泄漏——`resp.Body` 没关

```go
resp, err := http.Get(...)
// 少了:defer resp.Body.Close()
```

`http.Response.Body` 必须关闭,否则 TCP 连接不释放、连接池耗尽,服务跑久了泄漏。
**这是 Go review 里出现频率最高的点之一**:查 `http.Get` / `os.Open` / `sql.Rows` 后有没有 `defer ...Close()`。

### 坑 4:循环变量捕获

```go
for _, id := range ids {
    go func() {
        fetchUser(id)   // ← 捕获的是循环变量 id
    }()
}
```

Go **1.22 之前**,所有 goroutine 共享同一个 `id`,循环结束后它停在**最后一个值** →
所有 goroutine 全去拉同一个(最后一个)id。1.22+ 已修,但很多生产项目还锁在老版本。

> 这就是前端的老人:`for (var i=0;i<3;i++) setTimeout(()=>console.log(i))` 打印 `3 3 3`。
> Go 和 JS 在这件事上是亲兄弟。修法也熟悉——显式传参:
> ```go
> go func(id int) { fetchUser(id) }(id)   // ≈ JS 里把 var 换成 let
> ```

### 🎣 诱饵:`return &u`

```go
if u, ok := cache[id]; ok {
    return &u   // 看着像"返回局部变量地址"的经典悬垂指针 bug
}
```

**它完全安全**。在 C 里返回局部变量地址是 use-after-free,但在 Go 里编译器做
**逃逸分析**,会把 `u` 分配到堆上。

这个诱饵考的是:**不能"见 `_` 就报""见取地址就报"**。判断力比挑刺重要。

### 汇总

| 类别 | 位置 | 后果 | 触发条件 |
|---|---|---|---|
| 并发竞态 | map 读写 + slice 并发 append | 崩溃 / 丢数据 | 并发跑时 |
| 错误处理 | `if err != nil` 没 return | nil 解引用 panic | 请求失败时 |
| 资源泄漏 | 缺 `defer resp.Body.Close()` | 连接池耗尽 | 持续请求后 |
| 循环变量 | `go func(){ ...id... }()` | 全拉同一个 id | Go < 1.22 |
| 🎣 诱饵 | `return &u` | 无,安全 | 永远 |

---

## 四、第二轮:数据库查询 handler(6 坑 + 1 诱饵)

### 题目代码

```go
func ordersHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")

	ctx, _ := context.WithTimeout(r.Context(), 3*time.Second)

	query := fmt.Sprintf("SELECT id, user_id, amount FROM orders WHERE user_id = %s", userID)
	rows, err := db.QueryContext(context.Background(), query)
	if err != nil {
		fmt.Println("query error:", err)
		return
	}

	var orders []Order
	for rows.Next() {
		var o Order
		rows.Scan(&o.ID, &o.UserID, &o.Amount)
		orders = append(orders, o)
	}

	json.NewEncoder(w).Encode(orders)
}
```

### 坑 1:SQL 注入

```go
query := fmt.Sprintf("... WHERE user_id = %s", userID)
```

`userID` 直接来自 URL 参数,拼进 SQL。`?user_id=1 OR 1=1`、`?user_id=1; DROP TABLE orders--` 都能打。

> 前端类比:等于 `innerHTML` 直接拼用户输入 → XSS。同一个病:**把数据当代码**。

**修**:参数化,让驱动去转义:

```go
const query = `SELECT id, user_id, amount FROM orders WHERE user_id = ?`
rows, err := db.QueryContext(ctx, query, userID)
```

### 坑 2:context 建了但没用上

```go
ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)  // 建了 3 秒超时
rows, err := db.QueryContext(context.Background(), query)       // ← 却传 Background
```

辛苦建的 `ctx` 根本没用,查询走的是 `context.Background()`(永不超时)。后果:慢查询
永远不超时,连接和 goroutine 一直被占,并发上来雪崩。

> 前端类比:建好了 `AbortController`,但 `fetch` 时忘了传 `signal`——abort 永远不生效。

### 坑 3:cancel 被丢弃(资源泄漏)

```go
ctx, _ := context.WithTimeout(...)
//    ↑ 丢的不是 error,是 cancel 函数!
```

`WithTimeout` 返回 `(ctx, cancel)`,第二个是**取消函数**。丢进 `_` → 提前 return 时那个
定时器不释放 → 泄漏。`go vet` 会直接报 `the cancel function is not used on all paths (possible context leak)`。

**修**:`ctx, cancel := ...; defer cancel()`。

### 坑 4:rows 没关

```go
rows, err := db.QueryContext(...)
// 少了:defer rows.Close()
```

`Query*` 返回的 `rows` 必须关,否则**连接不还给连接池**,跑一阵池耗尽,全服挂起。
和第一轮 `resp.Body` 同一类病。**循环后还要查 `rows.Err()`**——`rows.Next()` 返回 false
只能说明"没有下一行了",不区分"真读完"还是"中途出错"。

```go
defer rows.Close()
for rows.Next() { ... }
if err := rows.Err(); err != nil { ... }
```

### 坑 5:空结果返回 `null`,前端崩

```go
var orders []Order        // nil slice
json.NewEncoder(w).Encode(orders)   // nil slice → JSON 的 null,不是 []
```

查不到订单时序列化成 `null`,前端 `orders.map(...)` → `Cannot read properties of null` → 白屏。

**修**:`orders := []Order{}`(空但非 nil → `[]`)。

> 这是前端视角最敏感的一条——接口某天返回 `null` 你一定见过。**你的前端经验在这里是优势位。**

### 坑 6:Scan 的错误被吞

```go
rows.Scan(&o.ID, &o.UserID, &o.Amount)   // 返回值没接
```

`Scan` 失败(比如某列是 NULL 扫进非空类型)时静默返回 error,你不接 → 订单带零值混进结果,
`Amount` 变 `0`,你还不知道数据是错的。**修**:`if err := rows.Scan(...); err != nil { return }`。

### 🎣 诱饵:`ctx, _ :=` 里"被忽略的 error"

新手见 `_` 就喊"你忽略了 error!"——**不算 bug**。`context.WithTimeout` 的 error 返回值
**永远是 `nil`**(签名为了统一才保留),忽略它 100% 是官方推荐写法。

**同一个 `_`,丢 error 是安全惯例,丢 cancel 是泄漏**——这就是为什么不能"见 `_` 就报"。

### 汇总

| # | 类别 | 位置 | 后果 | 触发条件 |
|---|---|---|---|---|
| 1 | SQL 注入 | `fmt.Sprintf` 拼 SQL | 拖库/删库 | 特定 URL 参数 |
| 2 | context 失效 | `QueryContext(context.Background(), ...)` | 慢查询雪崩 | 查询变慢时 |
| 3 | 资源泄漏 | 丢弃 `cancel` | 定时器泄漏 | 提前返回时 |
| 4 | 资源泄漏 | 缺 `defer rows.Close()` | 连接池耗尽 | 持续请求后 |
| 5 | 序列化 | `var orders []Order` | 前端 `null.map()` 崩 | 查不到数据时 |
| 6 | 错误处理 | `rows.Scan` 没接 err | 脏数据混入 | 列有 NULL 时 |
| 🎣 | 诱饵 | `WithTimeout` 的 error 被忽略 | 无 | 永远安全 |

### 修正版(当模板用)

```go
func ordersHandler(w http.ResponseWriter, r *http.Request) {
	// 信任边界:URL 参数先校验再进业务
	userID, err := strconv.Atoi(r.URL.Query().Get("user_id"))
	if err != nil {
		http.Error(w, "invalid user_id", http.StatusBadRequest) // 400,不是默认 200
		return
	}

	// 从请求 ctx 派生超时,defer cancel 归还定时器
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	// 参数化,绝不拼字符串
	const query = `SELECT id, user_id, amount FROM orders WHERE user_id = ?`

	// ctx 传进去(不是 Background),超时才生效
	rows, err := db.QueryContext(ctx, query, userID)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close() // 拿到 rows 立刻挂关闭

	orders := []Order{} // 空切片,序列化成 [] 而非 null
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.UserID, &o.Amount); err != nil {
			http.Error(w, "scan failed", http.StatusInternalServerError)
			return
		}
		orders = append(orders, o)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "iterate failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(orders)
}
```

### 概念补充 1:`context.Background()` 是什么

`Context` 是**穿过整条调用链的"控制信号总线"**,携带三样东西:**取消信号、超时/截止时间、
请求级数据**。它解决的是"一个请求带着一串子调用,中途用户断开/超时了,怎么让所有在跑的活一起停"。

Context 是一棵**树**,不能凭空造,必须从父节点派生。`context.Background()` 就是**最底层那个
空的根**——无父、不取消、不超时、不带数据,专门用于"我确实没有父上下文"的场景(如 `main()`)。

```go
ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
//        父 ─┘                        └ 派生:在父基础上加 3 秒超时,父取消子也跟着取消
```

| 函数 | 加的东西 |
|---|---|
| `WithCancel` | 能手动取消 |
| `WithTimeout` | N 秒后自动取消 |
| `WithDeadline` | 某个时刻后自动取消 |
| `WithValue` | 塞一个键值 |

**前端类比:就是 `AbortSignal`。**

| Go | 前端 |
|---|---|
| `ctx context.Context` 参数 | `{ signal }` |
| `context.Background()` | 一个永不 abort 的 controller |
| `context.WithTimeout(parent, 3s)` | `AbortSignal.timeout(3000)` 跟父信号合并 |
| `ctx` 必须显式当参数往下传 | `signal` 也得显式传进 `fetch` |

关键在**"显式传"**:Go 没有全局"当前上下文",必须像传 `signal` 一样一层层传下去。
坑 2 的本质就是:**信号建好了,线没接上。**

### 概念补充 2:`rows.Scan` 是什么

`rows` **不是整个结果集数组,而是一个指向当前行的游标(迭代器)**。分工:

- **`rows.Next()`** 控制"移动":还有下一行吗?有就移过去,返回 `true`。
- **`rows.Scan(&a, &b, &c)`** 控制"读取":把**当前这一行**的列,按顺序拆进你给的变量。

```go
for rows.Next() {                            // 翻页
    var o Order
    rows.Scan(&o.ID, &o.UserID, &o.Amount)   // 读当前行的三列
    orders = append(orders, o)
}
```

> 前端类比:
> ```js
> for (const row of rows) {            // Next()
>   const [id, userId, amount] = row   // Scan():按位置解构当前行的列
> }
> ```

**为什么是 `&o.ID` 加 `&`**:Go 是值传递。写 `Scan(o.ID)` 它拿到的是副本,改副本你的变量纹丝不动。
必须传**指针**,让它顺着地址写回真正的变量(类比:给它一个可写的 ref,它往里塞)。

**两个坑**:

1. **列顺序必须对**(按位置匹配)
   ```sql
   SELECT id, user_id, amount          -- 位置 1,2,3
   ```
   ```go
   rows.Scan(&o.ID, &o.UserID, &o.Amount)  // 必须一一对应
   ```
   若改了 SQL 列顺序却没改 Scan → **静默串数据**(改了 SQL 必须联动改 Scan)。

2. **NULL 会炸**(就是坑 6):某列是 NULL 而目标是 `float64`/`int` 等非空类型,
   `Scan` 返回 error。不接的后果不是崩溃,而是**拿到带零值的假数据**。要能接 NULL 得用
   `sql.NullFloat64` 或序列化层的 `*float64`。

---

## 五、第三轮:job worker 池(4 坑 + 1 诱饵)

### 题目代码

```go
func runJobs(jobs []Job, workers int) []int {
	jobCh := make(chan Job)
	results := make(chan int)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		go func() {
			wg.Add(1)
			defer wg.Done()
			for j := range jobCh {
				res, err := process(j)
				if err != nil {
					fmt.Println("error:", err)
					continue
				}
				results <- res
			}
		}()
	}

	for _, j := range jobs {
		jobCh <- j
	}

	wg.Wait()
	close(results)

	var out []int
	for r := range results {
		out = append(out, r)
	}
	return out
}
```

### 坑 1:`wg.Add(1)` 放错位置

```go
go func() {
    wg.Add(1)          // ← 在 goroutine 里
    defer wg.Done()
```

正确姿势是 **`go` 之前 Add**:`wg.Add(1); go func() { defer wg.Done(); ... }()`。

**为什么**:`wg.Add` 必须 happens-before `wg.Wait`。放 goroutine 里 → 主 goroutine 可能
在 Add 前就到 Wait(计数还是 0)→ Wait 直接返回 → `close(results)` 提前执行 → worker 还去
`results <- res` → **`panic: send on closed channel`**。

**更深的点**(资深视角):在这段具体代码里,这个竞态**被别的 bug 掩盖了**——主 goroutine
卡在 `jobCh <- j` 的发循环里,要等 worker 接收才能往下走,而 worker 接收前必然已执行了
`Add`。所以"它没发作"只是被坑 2/4 挡住了。`go vet` 会报它,**改对模式,别赌它不发作**。

### 坑 2:`jobCh` 没关

```go
for _, j := range jobs {
    jobCh <- j
}
// 缺 close(jobCh)
```

`close(jobCh)` 应在**发完所有 job 后**由**发送方**调用。不关 → worker 里的
`for j := range jobCh` 永远等不到"没有更多了" → **goroutine 永久阻塞 → 泄漏**,
`wg.Wait()` 也永不返回。

**channel 归属原则:谁发送,谁关闭**。别让接收方 close(会 panic)。

### 坑 3:错误被吞

```go
res, err := process(j)
if err != nil {
    fmt.Println("error:", err)   // 只打印
    continue                     // 结果被静默丢弃
}
```

worker 只 print 就 `continue`,失败 job 的结果凭空消失;而且函数签名是 `[]int`,
**没有 error 出口**,调用者永远不知道少了几个。和第一轮、第二轮的吞错同一个病:
**错误处理的方向必须是往上抛,不是就地打印**。修:用 `errCh` 或结果结构体带出去。

### 坑 4:无缓冲 channel 死锁(真凶)

```go
results := make(chan int)      // 无缓冲
...
results <- res                 // worker 发
...
wg.Wait()                      // 主 goroutine 在这等
for r := range results { ... } // 才开始收 —— 太晚了
```

**无缓冲 channel 的发送必须有接收方同时在收**。推一遍流程:

1. worker1 处理完,执行 `results <- res` → **没人收,卡住**;
2. worker2 也卡在 `results <- res`;
3. 主 goroutine 还在 `jobCh <- job` 发下一个 → 但两个 worker 都卡住,**没人接收 jobCh**,主也卡住;
4. 三方全卡 → **`fatal error: all goroutines are asleep - deadlock!`**

**根本错误**:写成了"**先等所有 worker 结束,再去收结果**"。但 worker 结束的前提是"结果被收走"
——**互相等,死锁**。生产者和消费者必须**并发跑**。

**修法**:`results` 带足缓冲,或**在单独的 goroutine 里 `wg.Wait()` + `close`**,让主 goroutine
腾出来收。

### 🎣 诱饵:主 goroutine 里的 append

```go
var out []int
for r := range results {
    out = append(out, r)   // 看着像第一轮的并发 append 竞态?
}
```

**安全**。这个循环只在**主 goroutine** 里跑,`out` 从头到尾只有一个 goroutine 碰。
第一轮的竞态是因为多个 goroutine 并发 append;这里没有。

诱饵考的:**按"有没有并发"判断,而不是按"长得像不像"判断**。

### 汇总

| # | 类别 | 位置 | 后果 |
|---|---|---|---|
| 1 | 同步原语 | `wg.Add` 在 goroutine 内 | 竞态:偶发 send on closed channel |
| 2 | channel 生命周期 | 缺 `close(jobCh)` | worker 永久阻塞 / 泄漏 |
| 3 | 错误处理 | `process` err 只打印 | 失败结果静默丢失 |
| 4 | 死锁 | 无缓冲 `results` + 先 Wait 后收 | 必挂 deadlock |
| 🎣 | 诱饵 | 主 goroutine 里的 append | 无,安全 |

### 修正版

```go
func runJobs(jobs []Job, workers int) []int {
	jobCh := make(chan Job)
	results := make(chan int)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1) // Add 在 go 之前
		go func() {
			defer wg.Done()
			for j := range jobCh {
				res, err := process(j)
				if err != nil {
					fmt.Println("error:", err) // 生产建议用 errCh 传出去
					continue
				}
				results <- res
			}
		}()
	}

	// 单独一个 goroutine 发 job,发完关闭 jobCh(谁发送谁关闭)
	go func() {
		for _, j := range jobs {
			jobCh <- j
		}
		close(jobCh)
	}()

	// 单独一个 goroutine 收尾:等 worker 全结束再关 results
	go func() {
		wg.Wait()
		close(results)
	}()

	// 主 goroutine 专心消费,和 worker 并发跑,破解无缓冲死锁
	var out []int
	for r := range results {
		out = append(out, r)
	}
	return out
}
```

改动的灵魂只有一句:**让"等 worker 结束"和"收结果"在两拨人手里同时进行,别让同一个人先后干。**

### 修正版的运行轨迹(脑内推演一遍)

给协程起名:worker A、worker B,发送者 C,收尾者 D。逐帧推演:

1. 进入 `runJobs`,创建 `jobCh`、`results` 和 WaitGroup
2. 循环 workers,创建协程 A、B;再创建发送协程 C;再创建收尾协程 D。**主协程启动完这三个后自己去 `range results` 消费,全程不等**
3. 后台四个任务并发:2 个 worker、1 个发送者、1 个收尾的 D,加上主协程一共 **5 个执行流**
4. A、B 卡在 `j := range jobCh`——因为 jobCh 里**没有数据**(注意:卡住的原因是"没数据",不是"没 close";close 是让 range **退出**的信号,不是放行的信号)
5. C 每写入一个 job,AB 中**恰好一个**被唤醒处理;C 发完一个要等 worker 来接才能发下一个(无缓冲,发送也卡)
6. worker 处理完执行 `results <- res`,这里也要卡住等主协程来收——整条链是 `C --jobCh--> AB --results--> 主协程`,**两级握手,任何一环没人接,发送方就卡住**
7. C 发完所有 job 后 `close(jobCh)`,AB 的 range 退出,各自 `wg.Done`
8. D 的 `wg.Wait()` 等计数器归零后 `close(results)`。注意 D 不是在 jobCh 关闭后才创建的——它一开始就启动了,只是挂在 `wg.Wait()` 上干等
9. 主协程卡在 `r := range results`,直到 D 关闭 results 才退出循环(close 不丢数据,主协程会把剩余的收完再走)
10. 主协程继续,返回 `out`

责任链一图流:

```
C 发完 → close(jobCh) → worker 的 range 退出 → wg 计数归零
      → D 的 wg.Wait() 返回 → close(results) → 主协程的 range 退出
```

两个容易答错的点(自测):

- **"谁卡死了主协程?"** 没有谁。C 卡的是自己(等 worker 接收);主协程启动完 C、D 就去消费了。
  真正"卡"主协程的只有一处:`range results` 等 D 关 channel——但这是正常等待,有人最终会来解锁,不是死锁。
- **无缓冲下 `wg.Wait()` 能放主协程里吗?** 不能。worker 卡在 `results <- res` 等主协程收,
  主协程却卡在 Wait 等 worker 退出——互相等,必死锁。所以必须像修正版这样,Wait 委托给 D,主协程腾出手来收。

---

## 六、拿走这套心智模型

审并发代码卡住时,问自己三句话:

1. **谁产生,谁关闭?**(channel 归属——发送方关闭,接收方绝不 close)
2. **发和收,是不是同时在跑?**(无缓冲会不会互等 → 死锁)
3. **计数/信号,有没有 happens-before 保证?**(`Add` 要在 `Wait` 前、signal 要在等之前)

审资源类代码卡住时,记住**"defer 配对哲学"**——拿到什么就 defer 还什么:

| 拿到 | 还 |
|---|---|
| `context.WithTimeout` | `defer cancel()` |
| `db.Query*` | `defer rows.Close()` |
| `http.Get` | `defer resp.Body.Close()` |
| `os.Open` | `defer f.Close()` |

这一条能挡掉未来 80% 的资源类 review 意见。

## 七、审查 checklist(扫 AI 的 Go 代码直接用)

### 并发 / 同步
- [ ] 共享的 map / slice 是否只在单个 goroutine 里读写?多个 goroutine 就得上锁或换 `sync.Map`
- [ ] `wg.Add` 是否都在 `go` **之前**调用?
- [ ] channel:发送方是否关闭了?接收方有没有去 close(错)?
- [ ] 无缓冲 channel 的生产者和消费者是否**并发**跑?会不会互等死锁?
- [ ] `for ... go func(){ 用循环变量 }()` —— 循环变量有没有显式传参?(Go < 1.22)
- [ ] 每个新起的 goroutine panic 会不会拖垮整个进程?该 recover 的 recover 了吗?

### 资源
- [ ] 每个 `Query*` / `http.Get` / `os.Open` 后面有 `defer ...Close()` 吗?
- [ ] `context.WithTimeout` / `WithCancel` 的 `cancel` 有 `defer cancel()` 吗?
- [ ] `rows.Err()` 查了吗(区分"真读完"和"中途出错")?

### 错误处理
- [ ] 每个 `if err != nil` 都 `return`(或显式处理)了吗?有没有往下走导致 nil 解引用?
- [ ] 有没有"只 print 不返回",把错误吞掉的?
- [ ] 错误有没有往上抛的出口(返回 error / errCh),而不是就地消失?

### 安全 / 信任边界
- [ ] SQL 是不是参数化(`?`),没有 `fmt.Sprintf` 拼接?
- [ ] URL / 请求参数进业务前校验了吗?
- [ ] 错误详情有没有直接返回给前端(可能泄露表名/SQL/连接串)?

### 序列化 / 语义边界
- [ ] nil slice / 空结构体序列化会变 `null` 还是 `[]`?前端能不能吃?
- [ ] `rows.Scan` 参数顺序和 SELECT 列顺序一致吗?改了 SQL 有没有联动改 Scan?
- [ ] 可能为 NULL 的列,目标变量是可空类型吗?

### 接口设计(设计意见,不是 bug)
- [ ] 该返回 `(T, error)` 的地方,是不是只返回了 `T`?
- [ ] **别把设计偏好当运行时 bug 报**——区分"我不喜欢"和"它会炸"。

---

> **一句话收尾**:AI 让你**不用写** Go,但没让你**不用懂** Go。真正要练的不是"写"的能力,
> 是"**审和兜底**"的能力——三句话(谁产生谁关闭 / 发收是否并发 / happens-before)、
> 一张 checklist、外加"诱饵"训练出的判断力。练到"扫一眼就能说清哪行会炸",你就是审 Go 的人。
