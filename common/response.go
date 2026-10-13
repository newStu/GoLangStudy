package common

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Response 统一响应结构，所有接口都返回这个格式
type Response struct {
	Code    int    `json:"code"`    // 业务码，学习阶段直接复用 HTTP 状态码（200/400/401/500）
	Message string `json:"message"` // 提示信息
	Data    any    `json:"data"`    // 数据载荷，失败时为 null
}

// Success 成功响应
func Success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Response{Code: http.StatusOK, Message: "success", Data: data})
}

// Fail 失败响应，code 同时作为 HTTP 状态码和业务码
func Fail(c *gin.Context, code int, msg string) {
	c.JSON(code, Response{Code: code, Message: msg})
}
