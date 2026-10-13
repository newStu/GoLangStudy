# 数据库接入与 GORM

> 本篇对应 `config/config.go`、`database/db.go`、`model/user.go`、`dao/user_dao.go`，深入记录 Web 项目接入 MySQL 的细节：DSN、连接池、模型映射、GORM 常用 API 和踩过的坑。分层架构的全貌见上一篇《登录接口与分层架构》。

## 一、DSN：连接串的每一部分

```go
const MySQLDSN = "golang:golang123456@tcp(127.0.0.1:3306)/golang_study?charset=utf8mb4&parseTime=True&loc=Local"
```

拆开看：

| 片段 | 含义 |
|---|---|
| `golang:golang123456` | 用户名:密码（专用账号，只有 golang_study 库的权限） |
| `tcp(127.0.0.1:3306)` | 走 TCP 连到本机 3306 端口（MySQL 跑在 Docker 容器里，端口映射到了宿主机） |
| `/golang_study` | 默认库名，之后的查询不用再写库名前缀 |
| `charset=utf8mb4` | 连接字符集；**mb4** 才能存 emoji，utf8（utf8mb3）是残缺的三字节实现 |
| `parseTime=True` | 把 MySQL 的 `DATETIME` 自动扫描成 Go 的 `time.Time` |
| `loc=Local` | 时间按本地时区解析，不加可能差 8 小时 |

两个容易踩的点：

- **密码含特殊字符要 URL 编码**：密码里如果有 `@`、`/`、`:` 等，必须写成 `%40`、`%2F`、`%3A`，否则 DSN 解析会错位——`@` 后面的内容会被当成主机地址。
- **`parseTime` 忘了加**：查出来的时间字段是 `[]byte`，扫描到 `time.Time` 字段直接报错，报错信息还不太直观。

实际项目里 DSN 不写死在代码里（密码不能进 git），常见做法是配置文件 + 环境变量覆盖；学习阶段先放 `config` 包集中管理，至少全项目只有这一处要改。

## 二、初始化连接：database/db.go 逐行

```go
var DB *gorm.DB

func InitDB() {
	db, err := gorm.Open(mysql.Open(config.MySQLDSN), &gorm.Config{})
	if err != nil {
		log.Fatal("连接 MySQL 失败: ", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		log.Fatal("获取底层 sql.DB 失败: ", err)
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	DB = db
}
```

### 2.1 gorm.Open 做了什么

`gorm.Open` 用驱动（`mysql.Open(dsn)`）建立连接并做一次 ping 验证，返回 `*gorm.DB`。它失败要立刻 `log.Fatal` 退出——数据库连不上的服务没有"降级运行"可言，带病启动只会让问题更晚暴露。这也是 `main.go` 里把 `database.InitDB()` 放在 `router.InitRouter()` 之前的原因。

### 2.2 两层对象：gorm.DB 和 sql.DB

`*gorm.DB` 是 ORM 的抽象（拼 SQL、做扫描），底层真正管连接的是标准库的 `*sql.DB`（连接池）。所以要设置连接池参数时，得先 `db.DB()` 把底层对象捞出来：

```go
sqlDB, _ := db.DB()
```

### 2.3 连接池三个参数

数据库连接是昂贵的资源（TCP 握手 + 认证），所以用"池"来复用：

| 参数 | 本项目值 | 含义 |
|---|---|---|
| `SetMaxIdleConns(10)` | 10 | 池里最多保留多少个**空闲**连接。太小的话，高峰期一放回就被关掉，下次请求又要重新建连 |
| `SetMaxOpenConns(100)` | 100 | 同时最多打开多少连接。这是对数据库的自我保护——不设上限的话，流量洪峰会把 MySQL 的 `max_connections` 打满，拖垮所有连它的服务 |
| `SetConnMaxLifetime(time.Hour)` | 1 小时 | 一条连接最长活多久，到期自动重建。防止长期持有同一连接被防火墙/代理静默掐断（表现为跑几小时后莫名 `broken pipe`） |

这三个值没有银弹，取决于业务的 QPS 和数据库承受力；学习项目用 `10/100/1h` 这组保守值即可。

### 2.4 全局变量 vs 依赖注入

本项目用包级全局变量 `database.DB`，dao 层直接引用，最直观。实际项目更常见**依赖注入**：main 里创建 `*gorm.DB`，构造 dao 结构体时传进去：

```go
// 注入式写法（本项目未采用，理解即可）
type UserDAO struct {
	db *gorm.DB
}
func NewUserDAO(db *gorm.DB) *UserDAO { return &UserDAO{db: db} }
```

好处是 dao 不依赖全局状态，单测时可以传入 mock 实现。全局变量的代价是测试不友好、并发初始化顺序要自己保证；学习阶段取舍后选了简单。

## 三、模型与表的映射：model/user.go

```go
type User struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Username  string    `gorm:"size:64;uniqueIndex" json:"username"`
	Password  string    `gorm:"size:128" json:"-"`
	Nickname  string    `gorm:"size:64" json:"nickname"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
```

### 3.1 命名约定

- **表名**：GORM 默认把结构体名**复数**——`User` → `users`。想显式指定可以实现 `Tabler` 接口或用 `TableName()`。
- **列名**：字段蛇形转换——`Nickname` → `nickname`，`CreatedAt` → `created_at`，和手写的 `scripts/init.sql` 一致。
- `ID uint` 按约定映射为自增主键（`primaryKey` tag 显式声明更清晰）。

### 3.2 两套 tag 各管一头

- `gorm:"..."` 管**数据库**：`size:64` 列宽、`uniqueIndex` 唯一索引。
- `json:"..."` 管**接口输出**：字段在响应 JSON 里的名字。
- **`json:"-"`**：序列化时彻底隐藏。`Password` 存的是 bcrypt 哈希也不该外泄——在 model 上一次堵死，比在每个 handler 里记得"别带上密码"可靠。本项目登录接口的响应里看不到 password 字段，就是它在起作用。

### 3.3 零值陷阱（GORM 通病）

`ID uint` 是零值敏感的：`Create(&user)` 后 GORM 会自动回填自增 ID；反过来，用 `Updates` 更新时零值字段（`Nickname: ""`）默认**不会**进 UPDATE 语句，需要更新零值时要改用 `Select` 或 map。本项目暂无写接口，先记下这个坑。

## 四、查询：dao 层在用的和常用的

### 4.1 项目里实际用的

```go
err := database.DB.Where("username = ?", username).First(&user).Error
```

拆开看这条链：

- `Where("username = ?", username)`：条件。**永远用 `?` 占位符传参**，值会被参数化发送，不存在 SQL 注入；字符串拼接 SQL 是绝对禁区。
- `First(&user)`：按主键排序取第一条，**没有则返回 `gorm.ErrRecordNotFound`**——不是返回 `nil, nil`，这点和很多库不一样。
- `.Error`：GORM 的链式调用最后挂一个 `*gorm.DB`，错误统一放在 `.Error` 字段，`errors.Is(err, gorm.ErrRecordNotFound)` 判断。

### 4.2 常用 API 速查（本项目暂未用到，备查）

```go
// 查多条
var users []model.User
db.Where("age > ?", 18).Find(&users)          // 查不到返回空切片，不报 ErrRecordNotFound
db.Take(&user)                                 // 不排序取一条
db.Count(&n)                                   // 计数

// 写
db.Create(&user)                               // 插入，回填自增 ID
db.Model(&user).Update("nickname", "新昵称")    // 更新单列
db.Delete(&user)                               // 删除（软删除需加 gorm.DeletedAt 字段）

// 事务
db.Transaction(func(tx *gorm.DB) error {
	// tx 上做的操作要么全成功要么全回滚
	// 返回 nil 提交，返回 error 回滚
	return nil
})
```

`First/Find/Take` 的区别值得记：`First` 排序取一条、找不到报错；`Take` 不排序取一条、找不到报错；`Find` 找不到是"空结果"不是错误——按业务语义选，登录里"查无此人是错误"用 `First` 恰好合适。

## 五、踩坑记录

1. **Docker 里的 MySQL，宿主机连不上 root**：容器是共享的，`root@'%'` 和 `root@localhost` 可以是两个密码，宿主机连入走的是 `root@'%'`（来源 IP 是网桥 `172.17.0.1`）。解法是建专用账号 `golang`，只授 `golang_study` 库的权限——应用连库本来就不该用 root。
2. **docker exec 导 SQL 中文乱码**：客户端默认 latin1，必须 `--default-character-set=utf8mb4`，否则 UTF-8 中文被双重编码（HEX 里是 `C3A7...` 而非 `E7AE...`）。
3. **建表用 SQL 脚本还是 AutoMigrate**：GORM 的 `AutoMigrate` 能按 model 自动建表/加列，学习时直观；但它**不会删列改列**，团队协作时表结构演变没有版本可循。实际项目主流是迁移脚本（或 golang-migrate 这类工具），本项目选了手写 `scripts/init.sql`。

## 六、验证连接的快捷方式

改完 DSN 不确定通不通，不用起整个服务，两行就够：

```go
// 任意 _test.go 或临时 main 里
db, err := gorm.Open(mysql.Open(config.MySQLDSN), &gorm.Config{})
fmt.Println(err) // nil 就是通了
```

## 七、待办

- register 接口：实践 `Create` 与 bcrypt 入库
- 事务：多表写入时用 `db.Transaction`
- 池参数感知：以后压测时观察 `db.DB().Stats()` 的连接池命中情况
