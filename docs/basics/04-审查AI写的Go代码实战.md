# Go 笔记:审查 AI 生成的 Go 代码(实战五讲)

> 学 Go 的目标不是"会写"——AI 写得比人快。真正的目标是**会审**:AI 像一个手速极快、
> 从不提问、还特别自信的中级工程师,它能把功能实现出来,但**判断不了并发安不安全、
> 错误该不该吞、逻辑该不该存在**。这篇笔记把五轮"找 bug"实战整理成一套可复用的
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

## 二、五轮实战的公共敌人:三类 bug

五轮的坑看着五花八门,归到最后就三类,而且**恰好都是 AI 最爱犯、光看代码看不出来的**:

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

## 六、第四轮:带后台刷新的内存缓存(4 坑 + 1 诱饵)

> 换战场到并发三件套:**context 取消传播 / 锁粒度 / select 多路复用**。核心结论一句话:
> **函数签名写了 `ctx` 不算数,有人 `select` 它才算数。**

### 题目代码

```go
type Cache struct {
	mu   sync.Mutex
	data map[string]string
}

// Get:命中直接返回;miss 时回源(慢)
func (c *Cache) Get(ctx context.Context, key string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if v, ok := c.data[key]; ok {
		return v, nil
	}

	v, err := fetchFromSource(ctx, key)
	if err != nil {
		return "", err
	}
	c.data[key] = v
	return v, nil
}

func fetchFromSource(ctx context.Context, key string) (string, error) {
	time.Sleep(200 * time.Millisecond) // 模拟一次慢网络请求
	return "value-" + key, nil
}

// Refresh:后台定时刷新一批 key
func (c *Cache) Refresh(ctx context.Context, keys []string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	for {
		select {
		case <-ticker.C:
			for _, k := range keys {
				v, err := fetchFromSource(ctx, k)
				if err != nil {
					fmt.Println("refresh error:", err)
					continue
				}
				c.mu.Lock()
				c.data[k] = v
				c.mu.Unlock()
			}
		}
	}
}
```

### 坑 1:锁粒度——持锁做慢 I/O(**性能 bug,不是正确性**)

```go
c.mu.Lock()
defer c.mu.Unlock()                   // 锁一直握到函数结束
...
v, err := fetchFromSource(ctx, key)   // 慢操作(200ms)在【锁内】跑
```

`Get` 持锁到结束。一次 cache miss 时,`fetchFromSource` 那 200ms 全程占锁 → **其他所有 `Get`
(哪怕命中缓存的)全部阻塞等锁** → 缓存退化成**串行**。

**这是本轮唯一一个"不算错但必须报"的坑**:结果没错,吞吐塌了。

> 前端类比:`async` 逻辑里做了同步阻塞操作,把事件循环卡住 200ms,所有请求排队。

**修**:读命中用短锁,miss 时**释放锁再回源**,回源完再短锁写回:

```go
c.mu.RLock()
v, ok := c.data[key]
c.mu.RUnlock()
if ok { return v, nil }

v, err := fetchFromSource(ctx, key) // I/O 不持锁
if err != nil { return "", err }

c.mu.Lock()
c.data[key] = v
c.mu.Unlock()
```

### 坑 2:context 取消不传播

```go
func fetchFromSource(ctx context.Context, key string) (string, error) {
	time.Sleep(200 * time.Millisecond)   // 收下了 ctx,却一眼没看
	return "value-" + key, nil
}
```

`ctx` **收下却从没用**——"接口骗人"。调用方取消时 `ctx.Done()` 早 close 了,`Sleep` 照样睡满,
取消信号断在这里。

**修**:真正的 I/O 把 ctx 传到底:

```go
select {
case <-time.After(200 * time.Millisecond):
	return "value-" + key, nil
case <-ctx.Done():
	return "", ctx.Err()
}
```

### 坑 3:select 缺 `ctx.Done()` → goroutine 永久泄漏

```go
select {
case <-ticker.C:        // 只有这一路
	...
}
```

`Refresh` 也把 `ctx` 收下了,但 `select` 里**没有 `case <-ctx.Done()`** → `for` 循环**没有退出
路径** → goroutine **永远无法取消 → 泄漏**。又是"ctx 收了不用"。

**select 的本质是"多路等待,谁先来走谁"**,只挂 ticker 一路,就丢了"取消"这一路。**规范的
select 至少要有 `ctx.Done()` 这条路**,否则这个 goroutine 别人关不掉。

> 前端类比:`setInterval` 跑着,却没有对应的 `clearInterval`。

**修**:加一路 `case <-ctx.Done(): return`。

### 坑 4:ticker 没 Stop(资源泄漏)

```go
ticker := time.NewTicker(interval)
// 少了:defer ticker.Stop()
```

`NewTicker` 的资源不 Stop 就不回收。**`NewTicker` ↔ `defer ticker.Stop()`** 是 defer 配对哲学
的一条。

### 🎣 诱饵:map 不需要 `sync.Map`

学过第一轮"map 并发会崩"的人容易反射性喊"换 `sync.Map`"——**错**。第一轮崩是因为 map
**没加锁**;这里 `data` 的每次读写都在 `c.mu` 保护下,**已经并发安全**。

> `sync.Map` 只在"读多写少且键不重叠"的特定场景才划算;**普通场景 `Mutex` + 普通 map 才是
> 标准做法**。诱饵考的是:把"map 要加锁"这个正确结论,和"必须用 sync.Map"这个错误做法分开。
>
> (换 `RWMutex` + 读锁是**优化**,不是 bug——别当错误报。)

### 汇总

| # | 类别 | 位置 | 后果 | 性质 |
|---|---|---|---|---|
| 1 | 锁粒度 | 持锁做 `fetchFromSource` | 缓存退化成串行 | 性能 bug |
| 2 | context 传播 | `fetchFromSource` 忽略 ctx | 取消信号断掉 | 正确性 |
| 3 | select | 缺 `case <-ctx.Done()` | goroutine 永久泄漏 | 正确性 |
| 4 | 资源 | 缺 `defer ticker.Stop()` | ticker 泄漏 | 正确性 |
| 🎣 | 诱饵 | map"并发不安全" | 无,已被 mu 保护 | 安全 |

**坑 2、3 是同一个病**:ctx 收下却从不 observe。判断"取消能不能传播",就看链上每一环有没有
真的读 `ctx.Done()`——**签名写 `ctx` 不算数,有人 select 它才算数**。

### 修正版

```go
type Cache struct {
	mu   sync.RWMutex
	data map[string]string
}

func (c *Cache) Get(ctx context.Context, key string) (string, error) {
	c.mu.RLock() // 读:短锁
	v, ok := c.data[key]
	c.mu.RUnlock()
	if ok {
		return v, nil
	}

	v, err := fetchFromSource(ctx, key) // I/O 不持锁
	if err != nil {
		return "", err
	}

	c.mu.Lock() // 写:短锁
	c.data[key] = v
	c.mu.Unlock()
	return v, nil
}

func fetchFromSource(ctx context.Context, key string) (string, error) {
	select {
	case <-time.After(200 * time.Millisecond):
		return "value-" + key, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (c *Cache) Refresh(ctx context.Context, keys []string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, k := range keys {
				v, err := fetchFromSource(ctx, k)
				if err != nil {
					fmt.Println("refresh error:", err)
					continue
				}
				c.mu.Lock()
				c.data[k] = v
				c.mu.Unlock()
			}
		}
	}
}
```

> `ponytail:` 修正版 `Get` 的 miss 仍存在**惊群**——多个 goroutine 同时 miss 同一 key 会重复
> 回源。真上线可加 `singleflight.Group` 合并;当前规模够用,先不引入依赖。

---

## 七、第五轮:批量导入 CSV(4 坑 + 1 诱饵)

> 换战场到**错误处理 / 资源生命周期 / 信任边界**——"能跑但数据不对"的重灾区。
> 核心结论一句话:**错误的唯一合法去处是往上抛,不是 let it disappear。**

### 题目代码

场景:把目录下所有 CSV 导入数据库。

```go
// ImportDir 把 dir 下所有 csv 导入数据库,返回导入行数
func ImportDir(db *sql.DB, dir string) (count int, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("read dir: %v", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			return count, err
		}
		defer f.Close()

		r := csv.NewReader(f)
		records, err := r.ReadAll()
		if err != nil {
			continue
		}

		for _, rec := range records {
			row := Row{Name: rec[0], Email: rec[1]}
			if err := insert(tx, row); err != nil {
				return count, err
			}
			count++
		}
	}

	if err := tx.Commit(); err != nil {
		return count, err
	}
	return count, nil
}

func insert(tx *sql.Tx, row Row) error {
	_, err := tx.Exec("INSERT INTO users(name, email) VALUES(?, ?)", row.Name, row.Email)
	if err != nil {
		return fmt.Errorf("insert %s: %v", row.Email, err)
	}
	return nil
}
```

### 坑 1:`defer f.Close()` 写在循环内(资源泄漏)

```go
for _, e := range entries {
	f, err := os.Open(...)
	defer f.Close()          // ← defer 的执行时机是【函数返回】,不是循环体结束
}
```

一万个文件 → 一万个 fd 全挂在函数退出前才释放 → `too many open files`,导入到一半全挂。
报错点在 `os.Open`,看着像"系统限制",根因是这行 `defer`。

> **`defer` 的作用域边界是函数,不是代码块。**

**前端类比**:`for` 里读了一万个文件流,却把所有 `stream.close()` 攒到最后才调;或者
`for` 里 `addEventListener` 不 `remove`——同一个形状。

**修**:把单文件逻辑抽成函数,`defer` 跟着函数作用域自然收敛:

```go
n, err := importFile(tx, path)   // defer 在 importFile 内,每次调用结束就还
```

### 坑 2:CSV 解析错误被 `continue` 吞掉(数据静默丢失)

```go
records, err := r.ReadAll()
if err != nil {
    continue          // ← 这个文件整体跳过,外面一无所知
}
```

编码不对、格式坏掉的文件被悄悄跳过。整批导入"成功返回",`count` 少了几百行,
**没人知道少了、少了哪个文件**。

这和第一轮 `if err != nil` 只 print、第二轮 `rows.Scan` 不接 err、第三轮 `process` 只打印
是**同一个病**:

> **错误的唯一合法去处是往上抛(或明确记日志 + 计数上报),不是 `continue`。**

### 坑 3:`%v` 而不是 `%w`(错误链断了)

```go
return fmt.Errorf("insert %s: %v", row.Email, err)
```

打印出来很好看:`insert a@b.com: UNIQUE constraint failed`。但 **`%v` 是把 error 字符串化后
重新造了一个新 error,原始 error 从链上掉了**。调用方:

```go
errors.Is(err, sql.ErrNoRows)     // ← 永远 false
errors.As(err, &uniqueErr)        // ← 永远 false
```

**永远失效**,只能去字符串匹配错误消息——脆弱到升一版驱动就崩。

**修**:`%w` 保留链:

```go
return fmt.Errorf("insert %s: %w", row.Email, err)
```

> 前端类比:`throw new Error(\`insert failed: ${e.message}\`)` 把 `cause` 丢了,
> 还是 `throw new Error(msg, { cause: e })`。前者 `instanceof` / `e.cause` 全拿不到,
> 只剩一句给人看的字符串。
>
> **`%v` 给人看,`%w` 给 `errors.Is/As` 用。包装错误默认写 `%w`,除非你故意想切断判断。**

### 坑 4:`rec[1]` 没有长度校验(信任边界,直接 panic)

```go
row := Row{Name: rec[0], Email: rec[1]}   // rec 可能只有 1 列、甚至 0 列
```

CSV 是**外部输入**,不守规矩是常态。首行只有 1 列时,`csv.Reader` 把 `FieldsPerRecord`
定成 1,后面所有行都"合法"地只有 1 个字段 → `rec[1]` **index out of range → panic**。

不走 error 返回,**直接掀桌**:在 HTTP handler 里就是一次 500;panic 发生在无 recover 的
goroutine 里 → **整个进程挂掉**。

**修**:进业务前验形状。

```go
if len(rec) < 2 {
    return 0, fmt.Errorf("%s:%d: 需要 2 列,实际 %d 列", path, i+1, len(rec))
}
```

> **信任边界三件套**:URL 参数(第二轮)、文件内容(本轮)、第三方响应。凡是"外面来的",
> 进业务前先验形状——长度、类型、非空。

### 🎣 诱饵:`defer tx.Rollback()` 忽略了 error

```go
defer tx.Rollback()   // ← "返回值没接!"
```

**这是标准写法,别改。** `Commit` 成功后 `Rollback` 返回 `sql.ErrTxDone`——事务已经结束,
回滚无事可做,**这正是你要的**:

- Commit 失败 → Rollback 真的回滚;
- Commit 成功 → Rollback 空转,返回一个无害的错。

你要是把它接住并上报,每次成功导入都会多一条假错误日志。

唯一要注意的是**位置**:必须在 `db.Begin()` 的 err 检查**之后**。放前面 → Begin 失败时
`tx` 是 nil → `defer` 里 nil pointer panic。

### 本轮出现的三个误判(比找对坑更值钱)

这三条是真实答题记录,都是"看着像 bug"的直觉,必须拧正:

**① "`os.Open` 失败也要 Close"** —— 错。失败时 `f` 是 nil,没有资源可关,`Close` 直接 panic。
真正的坑是 `defer` 的位置(坑 1),不是失败路径。

**② "`insert` 应该用协程跑"** —— 错,而且会引入新 bug:

1. **`*sql.Tx` 不是并发安全的**。Go 文档明写:一个事务上的操作必须串行。多 goroutine 往同一
   个 `tx` 里 `Exec` → 行为未定义。
2. **`count++` 会变成竞态**——就是第一轮那个坑,换个马甲。
3. **一个事务本来就串行**。数据库执行 INSERT 顺序提交,客户端开 10 个 goroutine,到服务端
   还是排队,只多了一堆错误合并与协调。

> **senior 的反射**:看到"循环里做事慢",先问**慢在客户端还是服务端**。是服务端,客户端开
> 协程只是把排队搬到本地。想快靠批量 `INSERT ... VALUES (?,?),(?,?)` 和减少事务往返。

**③ "`count` 在 commit 前就自增,返回值骗人"** —— **对,而且很 senior**。
`count++` 一路涨,`tx.Commit()` 失败时返回 `count=9999, err="commit failed"`,调用方可能
当"导了 9999 条"处理,**实际一条都没落库**。

> **修**:Commit 失败就 `return 0, err`。别让返回值说一半真话。

### 汇总

| # | 类别 | 位置 | 后果 | 触发条件 |
|---|---|---|---|---|
| 1 | 资源泄漏 | 循环内 `defer f.Close()` | `too many open files` | 文件数变多时 |
| 2 | 错误处理 | `ReadAll` err 后 `continue` | 整文件静默跳过,数据缺失 | CSV 编码/格式异常 |
| 3 | 错误链 | `fmt.Errorf` 用 `%v` | `errors.Is/As` 永远 false | 调用方想分支处理时 |
| 4 | 信任边界 | `rec[1]` 无长度校验 | index out of range panic | 畸形 CSV(首行列数少) |
| 5 | 返回值语义 | commit 前 `count++`,失败仍返回 count | 调用方误判导入成功量 | commit 失败时 |
| 🎣 | 诱饵 | `defer tx.Rollback()` 忽略 error | 无,标准姿势 | 永远安全 |

### 修正版

```go
func ImportDir(db *sql.DB, dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("read dir: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() // 成功后返回 ErrTxDone,无害

	count := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n, err := importFile(tx, filepath.Join(dir, e.Name()))
		if err != nil {
			return 0, err // 整体回滚,count 不作数
		}
		count += n
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return count, nil
}

func importFile(tx *sql.Tx, path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close() // 生命周期绑定本次调用,不累积

	records, err := csv.NewReader(f).ReadAll() // ponytail: 整文件入内存,超大文件改 Read() 流式
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", path, err)
	}

	n := 0
	for i, rec := range records {
		if len(rec) < 2 {
			return 0, fmt.Errorf("%s:%d: 需要 2 列,实际 %d 列", path, i+1, len(rec))
		}
		if err := insert(tx, Row{Name: rec[0], Email: rec[1]}); err != nil {
			return 0, err
		}
		n++
	}
	return n, nil
}

func insert(tx *sql.Tx, row Row) error {
	if _, err := tx.Exec("INSERT INTO users(name, email) VALUES(?, ?)", row.Name, row.Email); err != nil {
		return fmt.Errorf("insert %s: %w", row.Email, err)
	}
	return nil
}
```

改动只有 5 处:**defer 收进小函数、吞错改上报、`%v`→`%w`、列数校验、commit 失败不返回 count**。
业务逻辑一行没动。

### 本轮该带走的三条

1. **`defer` 的边界是函数,不是代码块。** 循环里拿到资源,要么显式关,要么把那段逻辑抽成
   函数让 defer 自然收敛。
2. **`%w` 是错误可编程的唯一方式。** `%v` 给人看,`%w` 给 `errors.Is/As` 用。
3. **循环 + 外部输入 = 必须验形状。** 长度、类型、非空,进业务前挡住。`panic` 不是错误处理。

---

## 八、拿走这套心智模型

审并发代码卡住时,问自己五句话:

1. **谁产生,谁关闭?**(channel 归属——发送方关闭,接收方绝不 close)
2. **发和收,是不是同时在跑?**(无缓冲会不会互等 → 死锁)
3. **计数/信号,有没有 happens-before 保证?**(`Add` 要在 `Wait` 前、signal 要在等之前)
4. **锁的持有范围有没有跨越 I/O?**(锁里只放"读/写内存",慢操作一律挪出去)
5. **ctx 从入口到出口,每一环都真的 observe 了吗?**(谁没 `select ctx.Done()`,取消就断在哪)

审错误与资源类代码时,再问三句话:

6. **每个 `err` 的出口在哪?**(`if err != nil` 之后是 `return` / 上报,还是 print / continue = 吞)
7. **`defer` 的边界是函数还是循环?**(循环里 `defer` = 堆积到函数返回才释放)
8. **包装错误用 `%v` 还是 `%w`?**(`%v` 断链,`errors.Is/As` 全部失效)

审资源类代码卡住时,记住**"defer 配对哲学"**——拿到什么就 defer 还什么:

| 拿到 | 还 |
|---|---|
| `context.WithTimeout` | `defer cancel()` |
| `db.Query*` | `defer rows.Close()` |
| `http.Get` | `defer resp.Body.Close()` |
| `os.Open` | `defer f.Close()` |

**但配对只是第一步,位置同样重要**:同一句 `defer f.Close()` 在函数开头是对的,在循环体里
就是 fd 泄漏。**defer 的执行时机永远是"当前函数返回",不是"当前代码块结束"。**

这一条能挡掉未来 80% 的资源类 review 意见。

## 九、审查 checklist(扫 AI 的 Go 代码直接用)

### 并发 / 同步
- [ ] 共享的 map / slice 是否只在单个 goroutine 里读写?多个 goroutine 就得上锁或换 `sync.Map`
- [ ] `wg.Add` 是否都在 `go` **之前**调用?
- [ ] channel:发送方是否关闭了?接收方有没有去 close(错)?
- [ ] 无缓冲 channel 的生产者和消费者是否**并发**跑?会不会互等死锁?
- [ ] `for ... go func(){ 用循环变量 }()` —— 循环变量有没有显式传参?(Go < 1.22)
- [ ] 每个新起的 goroutine panic 会不会拖垮整个进程?该 recover 的 recover 了吗?

### context 取消
- [ ] 每个接收 `ctx` 的函数,**真的用了它**吗?有没有"收下不用"的接口骗人?
- [ ] 所有可能阻塞的点(网络 / DB / sleep / channel 收发),都 `select` 了 `ctx.Done()` 吗?
- [ ] `select` 里是不是至少有一条 `case <-ctx.Done()`(否则 goroutine 关不掉)?
- [ ] `time.NewTicker` / `NewTimer` 有配对的 `defer Stop()` 吗?

### 锁 / 临界区
- [ ] 锁的持有范围**没有跨越任何 I/O** 吗?(网络 / 磁盘 / 外部调用一律挪到锁外)
- [ ] 只读路径考虑过 `RWMutex` 读锁吗(优化项,不是必须)?
- [ ] map 是否**始终**在同一把锁下读写?(加了锁就够,不必上 `sync.Map`)

### 资源
- [ ] 每个 `Query*` / `http.Get` / `os.Open` 后面有 `defer ...Close()` 吗?
- [ ] `defer ...Close()` 是不是写进了**循环体内**?(defer 到函数返回才执行 → fd 堆积)
- [ ] `context.WithTimeout` / `WithCancel` 的 `cancel` 有 `defer cancel()` 吗?
- [ ] `rows.Err()` 查了吗(区分"真读完"和"中途出错")?

### 错误处理
- [ ] 每个 `if err != nil` 都 `return`(或显式处理)了吗?有没有往下走导致 nil 解引用?
- [ ] 有没有"只 print 不返回"或"`continue` 掉",把错误吞掉的?
- [ ] 错误有没有往上抛的出口(返回 error / errCh),而不是就地消失?
- [ ] 包装错误用 `%w` 而不是 `%v` 了吗?(`%v` 断链 → `errors.Is/As` 永远 false)
- [ ] 失败路径上的返回值会不会"说一半真话"?(如 commit 失败仍返回累计 `count`)

### 事务
- [ ] `defer tx.Rollback()` 在 `db.Begin()` 的 err 检查**之后**吗?(放前面 → Begin 失败时 nil panic)
- [ ] 有没有把 `*sql.Tx` 丢给多个 goroutine 用?(事务必须串行,否则行为未定义)

### 安全 / 信任边界
- [ ] SQL 是不是参数化(`?`),没有 `fmt.Sprintf` 拼接?
- [ ] URL / 请求参数进业务前校验了吗?
- [ ] 外部内容(文件、CSV、第三方响应)进业务前**验形状**了吗?长度、类型、非空——`rec[1]` 会不会越界 panic?
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
> 是"**审和兜底**"的能力——八句话(谁产生谁关闭 / 发收是否并发 / happens-before /
> 锁有没有跨 I/O / ctx 每一环 observe 了吗 / err 的出口在哪 / defer 的边界是函数还是循环 /
> 包装用 `%v` 还是 `%w`)、一张 checklist、外加"诱饵"训练出的判断力。
> 练到"扫一眼就能说清哪行会炸",你就是审 Go 的人。
