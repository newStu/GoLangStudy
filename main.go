package main

import (
	"log"

	"GoLangStudy/database"
	"GoLangStudy/router"
)

const (
	DefaultPort = ":8080"
)

func main() {
	// 先初始化数据库连接，失败直接退出——没有数据库整个服务没有意义
	database.InitDB()

	r := router.InitRouter()
	err := r.Run(DefaultPort)
	if err != nil {
		log.Fatal(err)
	}
}
