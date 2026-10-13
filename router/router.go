package router

import "github.com/gin-gonic/gin"

func InitRouter() *gin.Engine {
	r := gin.Default()

	// 基础分组：所有业务路由统一挂在这个前缀下
	base := r.Group("/api/" + APIVersion)

	registerAuthRoutes(base)
	registerBusinessRoutes(base)

	return r
}
