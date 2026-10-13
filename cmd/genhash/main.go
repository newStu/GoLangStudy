// genhash 是一个小工具：对明文密码生成 bcrypt 哈希
// 用法：go run ./cmd/genhash <密码>
// 项目暂未做 register 接口，往 users 表手动插数据时用它生成 Password 字段
package main

import (
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Println("用法: go run ./cmd/genhash <密码>")
		os.Exit(1)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(os.Args[1]), bcrypt.DefaultCost)
	if err != nil {
		fmt.Println("生成哈希失败:", err)
		os.Exit(1)
	}
	fmt.Println(string(hash))
}
