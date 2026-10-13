package main

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
)

// ============================================================
// 案例 1:反面教材 —— 普通 map 被多个协程同时写,直接崩溃
// ============================================================
// 运行方式:go run . crash
//
// 注意:这不是 panic,是 runtime 抛出的 fatal error,**recover 拦不住**,
// 进程会整个挂掉。这就是"map 不是线程安全"的最直接证据。
func crashDemo() {
	m := make(map[int]int)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				m[n*1000+j] = j // ← 多个协程并发写同一个 map
			}
		}(i)
	}
	wg.Wait()
	fmt.Println("正常结束(实际上大概率到不了这里)")
}

// ============================================================
// 案例 2:解法一 —— sync.Mutex 互斥锁,最通用的写法
// ============================================================
type lockedCounter struct {
	mu sync.Mutex
	m  map[string]int
}

func (c *lockedCounter) Inc(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key]++ // 写操作全程持锁,绝对安全
}

func (c *lockedCounter) Get(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[key] // 注意:读也要加锁!并发"读写"同样会崩
}

func mutexDemo() {
	c := &lockedCounter{m: make(map[string]int)}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.Inc("url")
			}
		}()
	}
	wg.Wait()
	fmt.Printf("[Mutex]     50 个协程各加 100 次,结果 = %d\n", c.Get("url"))
}

// ============================================================
// 案例 3:解法二 —— sync.RWMutex 读写锁,读多写少时吞吐更高
// ============================================================
type rwLockedCache struct {
	mu   sync.RWMutex
	data map[string]string
}

func (c *rwLockedCache) Set(key, val string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = val
}

func (c *rwLockedCache) Get(key string) (string, bool) {
	c.mu.RLock()         // 读锁:多个读者可以同时进入
	defer c.mu.RUnlock()
	v, ok := c.data[key]
	return v, ok
}

func rwMutexDemo() {
	c := &rwLockedCache{data: make(map[string]string)}
	var wg sync.WaitGroup

	// 2 个写者
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.Set(fmt.Sprintf("key-%d-%d", n, j), "v")
			}
		}(i)
	}
	// 10 个读者并发读,互相不阻塞
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.Get("key-0-1")
			}
		}()
	}
	wg.Wait()
	fmt.Printf("[RWMutex]   2 写 + 10 读 并发完成,缓存条数 = %d\n", len(c.data))
}

// ============================================================
// 案例 4:解法三 —— sync.Map,标准库提供的并发安全 map
// ============================================================
func syncMapDemo() {
	var m sync.Map

	// Store:存
	m.Store("alice", 100)
	m.Store("bob", 200)

	// Load:取,注意返回 any,需要类型断言
	v, ok := m.Load("alice")
	if ok {
		fmt.Printf("[sync.Map]  Load alice = %d\n", v.(int))
	}

	// LoadOrStore:不存在才存,存在则返回已有值 —— 并发去重神器
	// 100 个协程同时给 "counter" 初始化,只有一个的 0 会真正写入
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.LoadOrStore("counter", 0)
		}()
	}
	wg.Wait()
	actual, loaded := m.LoadOrStore("counter", 0)
	fmt.Printf("[sync.Map]  LoadOrStore counter: 值=%v, 已存在=%v\n", actual, loaded)

	// Swap:原子地替换并返回旧值
	old, _ := m.Swap("alice", 150)
	fmt.Printf("[sync.Map]  Swap alice: 旧值=%v\n", old)

	// CompareAndSwap:值匹配才替换(CAS)
	swapped := m.CompareAndSwap("alice", 150, 999)
	fmt.Printf("[sync.Map]  CompareAndSwap(150->999) 成功=%v\n", swapped)

	// Delete / LoadAndDelete
	m.Delete("bob")
	v2, ok2 := m.LoadAndDelete("counter")
	fmt.Printf("[sync.Map]  LoadAndDelete counter: 值=%v, 存在=%v\n", v2, ok2)

	// Range:遍历(顺序随机!),可以在遍历中安全删除当前 key
	totalKeys := 0
	m.Range(func(key, value any) bool {
		totalKeys++
		fmt.Printf("[sync.Map]  Range: %v -> %v\n", key, value)
		return true // 返回 false 提前结束遍历
	})
	fmt.Printf("[sync.Map]  遍历后剩余 %d 个 key\n", totalKeys)
}

// ============================================================
// 案例 5:sync.Map 的典型场景 —— 并发安全的"接口调用次数统计"
// ============================================================
// 模拟:多个协程处理请求,统计每个接口被调用了多少次。
// 特点是"读多写多但 key 集合基本固定",适合 sync.Map。
type apiStat struct {
	Name  string
	Count int64 // 用原子操作累加,所以是 int64
}

func statsDemo() {
	var stats sync.Map // key: 接口名, value: *apiStat(存指针,才能原子累加)

 apis := []string{"/login", "/order", "/pay"}
	var wg sync.WaitGroup
	for _, api := range apis {
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(name string) {
				defer wg.Done()
				// LoadOrStore + 原子加:第一到达者初始化,其余拿到同一个指针
				val, _ := stats.LoadOrStore(name, &apiStat{Name: name})
				stat := val.(*apiStat)
				for j := 0; j < 100; j++ {
					// 注意:sync.Map 只保证"map 本身"的并发安全,
					// 里面存的值要自己保护 —— 这里用原子操作累加
					atomic.AddInt64(&stat.Count, 1)
				}
			}(api)
		}
	}
	wg.Wait()

	stats.Range(func(k, v any) bool {
		s := v.(*apiStat)
		fmt.Printf("[stats]     %-8s 被调用 %d 次(期望 1000)\n", s.Name, s.Count)
		return true
	})
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "crash" {
		crashDemo() // go run . crash  ← 想看崩溃效果时运行
		return
	}
	fmt.Println("========== Mutex 版本 ==========")
	mutexDemo()
	fmt.Println("========== RWMutex 版本 ==========")
	rwMutexDemo()
	fmt.Println("========== sync.Map 基础 API ==========")
	syncMapDemo()
	fmt.Println("========== sync.Map 实战:接口调用统计 ==========")
	statsDemo()
}
