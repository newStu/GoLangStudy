package main

import (
	"GoLangStudy/router"
)

const (
	DefaultProt = ":8080"
)

func main() {
	routersInit := router.InitRouter()
	err := routersInit.Run(DefaultProt)
	if err != nil {
		panic(err)
	}
}
