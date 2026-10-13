package main

import (
	"GoLangStudy/router"
)

const (
	DefaultPort = ":8080"
)

func main() {
	r := router.InitRouter()
	err := r.Run(DefaultPort)
	if err != nil {
		panic(err)
	}
}
