# Go 笔记:map 线程安全与 sync.Map

> 场景:多个协程往同一个 map 里读写,程序直接崩;换成 `sync.Mutex`、`RWMutex`、`sync.Map` 三种方式分别怎么写、各自适合什么时候用。
> 配套代码:`Demo/syncmap/index.go`(`go run ./Demo/syncmap` 看安全版本,`go run ./Demo/syncmap crash` 看崩溃版本)。

## 一、普通 map 并发读写,不是数据错乱,是直接崩溃

先明确一个反直觉的事实:**Go 的 map 并发写不是"算错数"这么温柔,而是整个进程挂掉**。

```go
m := make(map[int]int)
for i := 0; i < 100; i++ {
    go func(n int) {
        for j := 0; j < 1000; j++ {
            m[n*1000+j] = j // ← 100 个协程同时写
        }
    }(i)
}
```

运行结果:

```
fatal error: concurrent map writes
```

三个关键点:

1. **这是 fatal error,不是 panic**。runtime 直接把线程挂起然后退出,`recover()` 拦不住,`defer` 也不执行。
2. **并发"读+写"也会崩**(报 `concurrent map read and map write`)。因为写操作可能触发扩容、搬迁 bucket,此时另一个协程在读,读到的是搬迁到一半的底层结构。
3. 崩溃是**概率性**的 —— 数据量小、协程少时可能"看起来没事",上线流量一上来才炸。所以本地测过 ≠ 没问题。

排查工具:`go run -race main.go`,race detector 会在崩溃前就把竞争的协程和代码行报出来,不用等真的崩。

## 二、三种解法

### 1. sync.Mutex —— 最通用,无脑安全

```go
type lockedCounter struct {
    mu sync.Mutex
    m  map[string]int
}

func (c *lockedCounter) Inc(key string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.m[key]++
}

func (c *lockedCounter) Get(key string) int {
    c.mu.Lock()         // 读也要加锁!
    defer c.mu.Unlock()
    return c.m[key]
}
```

坑:**读也要加锁**。很多人只在写的时候加锁,觉得"读应该没事"——不行,并发读写照样崩。

### 2. sync.RWMutex —— 读多写少时吞吐更高

```go
type rwLockedCache struct {
    mu   sync.RWMutex
    data map[string]string
}

func (c *rwLockedCache) Get(key string) (string, bool) {
    c.mu.RLock()             // 读锁:多个读者可同时进入,互不阻塞
    defer c.mu.RUnlock()
    v, ok := c.data[key]
    return v, ok
}

func (c *rwLockedCache) Set(key, val string) {
    c.mu.Lock()              // 写锁:独占,读写都挡住
    defer c.mu.Unlock()
    c.data[key] = val
}
```

注意:RWMutex 的读锁在**有写者排队时,新读者会被挡住**(防止写者饿死),所以它不是万能提速,只有"读远多于写"才划算。写也频繁时,来回加解锁的开销可能比 Mutex 还慢。

### 3. sync.Map —— 标准库的并发安全 map

```go
var m sync.Map          // 零值可用,不需要 make,不要复制(不能作为值传递/嵌套拷贝)

m.Store("alice", 100)              // 存
v, ok := m.Load("alice")           // 取 → v 是 any,要类型断言 v.(int)
m.Delete("bob")                    // 删
old, loaded := m.LoadOrStore("k", 0)  // 没有才存,有则取回 → 并发去重神器
prev, _ := m.Swap("alice", 150)    // 原子替换并返回旧值
m.CompareAndSwap("alice", 150, 999) // 值等于旧值才替换(CAS)
val, existed := m.LoadAndDelete("k") // 取出并删除,一步到位

m.Range(func(k, v any) bool {      // 遍历,顺序随机!
    return true  // 返回 false 提前结束
})
```

## 三、sync.Map 的实现原理(面试常问)

`sync.Map` 内部维护两个结构:

```
type Map struct {
    mu     Mutex              // 只保护 dirty
    read   atomic.Pointer[readOnly]  // 无锁读,内含 map[any]*entry
    dirty  map[any]*entry     // 需要加锁访问
    misses int                // read 里没找到的次数
}
```

核心是空间换时间 + 读写分离:

1. **read**:无锁的只读 map,`Load` 优先走这里,纯原子操作,这是它快的原因。
2. **dirty**:新写入的 key 先进这里(加 mu 锁)。当 read 里找不到(misses 累计超过 dirty 长度)时,dirty 整体升级为新的 read,清空重建。
3. **entry**:read 和 dirty 里的 key 指向同一个 entry(指针),所以对已有 key 的更新(`Store` 覆盖)只需要原子改 entry 的指针,**不用碰 dirty、不用加锁**——这就是"更新已有 key 也快"的秘密。
4. **expunged**:删除已有 key 时,read 里的 entry 被标记为 `nil`(惰性删除),等下次升级 dirty 时才真正清理(expunged)。

由此推出它的性能特征:

| 操作 | 快慢 | 原因 |
|------|------|------|
| 读已有 key | ✅ 最快 | 纯原子操作,无锁 |
| 更新已有 key | ✅ 快 | 只原子换 entry 指针 |
| 写入新 key | ⚠️ 慢 | 要加锁操作 dirty |
| 删除 key | ⚠️ 一般 | 惰性删除,有额外开销 |

## 四、选型:什么时候用哪个

官方文档(`sync.Map` 的注释)给出的适用场景,翻译过来就是:

> 当 **读多写少且 key 集合基本固定**,或者 **多个协程各自写互不重叠的一组 key** 时,用 sync.Map 能比 Mutex+map 减少锁竞争。

| 场景 | 推荐 |
|------|------|
| 连接池、配置缓存、路由表、接口统计 | `sync.Map`(读多写少) |
| 每个协程只写自己那批 key(分片写入,一起读) | `sync.Map` |
| 写频繁、key 不断新增/删除 | `Mutex` + 普通 map |
| 读多写少但需要遍历/统计(len、批量操作) | `RWMutex` + 普通 map |
| 需要强类型、编译期检查 | `Mutex` + `map[K]V`(sync.Map 是 `any`) |

一句话:**sync.Map 不是"更好的 map",而是"为两个特殊场景优化的 map"**。写多场景用它反而更慢(每次新 key 都要过锁 + 可能触发 dirty 重建)。

## 五、踩坑记录

1. **值是 `any`,忘记类型断言会在用的时候 panic**:

   ```go
   m.Store("count", 10)
   n := m.Load("count").(int) // 取出 any,断言成 int
   ```

   存的值类型不一致时,`v.(int)` 直接 panic。稳妥写法 `v, ok := x.(int)`。

2. **没有 Len 方法**。想知道条数只能 `Range` 里自己数一遍。需要频繁取 len 的场景,用 RWMutex + map 更合适。

3. **Range 顺序随机**。和普通 map 一样,遍历顺序每次不同,不能依赖顺序逻辑。好在 Range 回调里删除当前 key 是安全的(惰性删除)。

4. **sync.Map 只保护 map 本身,不保护里面存的值**。这个最容易忽略:

   ```go
   val, _ := stats.LoadOrStore(name, &apiStat{Name: name})
   stat := val.(*apiStat)
   stat.Count++   // ← 数据竞争!sync.Map 管不到结构体内部字段
   ```

   正确做法:存指针,内部字段用 `sync/atomic` 操作(如 `atomic.AddInt64(&stat.Count, 1)`),或者字段本身用不可变值整体替换(`Store` 回去)。

5. **零值可用、不可复制**。`var m sync.Map` 直接用,不需要 make;但它内部有锁,不能值拷贝(传参时传指针,也不要嵌在会被拷贝的结构体里)。

6. **吞掉类型错误**。普通 map 写错类型编译就报错,sync.Map 要到运行时断言才炸,问题暴露得晚。Go 1.9 加入 sync.Map 时社区就有人建议包一层泛型(1.18+ 可以自己写 `type ConcurrentMap[K comparable, V any]` 封装),既有类型安全又有清晰 API。

## 六、总结

- 普通 map 并发读写 = `fatal error`,recover 救不了,`-race` 提前查。
- 三件套:**Mutex(通用)→ RWMutex(读多写少)→ sync.Map(读多写少 + key 稳定 / 分片写入)**。
- sync.Map 快的本质:read 无锁 + entry 指针共享,更新已有 key 不加锁。
- sync.Map 值是 `any`:断言、无 Len、内部值要自己保护,三个坑记住。
