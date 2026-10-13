package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// registerBusinessRoutes 注册业务相关路由
func registerBusinessRoutes(r *gin.RouterGroup) {
	api := r.Group("/api") // 业务路由统一挂到 /api 前缀下

	api.GET("/orders", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "订单列表"})
	})
	api.POST("/orders", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "创建订单"})
	})
}
