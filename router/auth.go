package router

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"GoLangStudy/handler"
)

// registerAuthRoutes 注册认证相关路由：注册、登录等
func registerAuthRoutes(r *gin.RouterGroup) {
	authHandler := &handler.AuthHandler{}
	auth := r.Group("/auth") // 所有认证路由统一挂到 /auth 前缀下

	// TODO: register 接口待实现，先保留占位
	auth.POST("/register", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "注册"})
	})
	auth.POST("/login", authHandler.Login)
}
