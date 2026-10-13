package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// registerBusinessRoutes 注册业务相关路由
func registerBusinessRoutes(r *gin.RouterGroup) {

	r.GET("/orders", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "订单列表"})
	})
	r.POST("/orders", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "创建订单"})
	})
}
