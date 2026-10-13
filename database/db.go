package database

import (
	"log"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"GoLangStudy/config"
)

// DB 是全局数据库连接对象，dao 层通过 database.DB 直接使用
// 学习项目用全局变量最直观；实际项目更常见的做法是依赖注入，
// 即在 main 里创建 DB 后逐层传给 dao 结构体，便于测试时替换成 mock
var DB *gorm.DB

// InitDB 初始化 MySQL 连接，程序启动时调用一次
func InitDB() {
	db, err := gorm.Open(mysql.Open(config.MySQLDSN), &gorm.Config{})
	if err != nil {
		log.Fatal("连接 MySQL 失败: ", err)
	}

	// 拿到底层 sql.DB 设置连接池参数
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatal("获取底层 sql.DB 失败: ", err)
	}
	sqlDB.SetMaxIdleConns(10)           // 空闲连接上限
	sqlDB.SetMaxOpenConns(100)          // 最大打开连接数
	sqlDB.SetConnMaxLifetime(time.Hour) // 连接最长复用时间，超时自动重建

	DB = db
}
