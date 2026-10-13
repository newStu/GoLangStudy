package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"GoLangStudy/common"
	"GoLangStudy/service"
)

// AuthHandler 认证相关接口的处理器
type AuthHandler struct{}

// loginRequest 登录请求参数
// binding:"required" 配合 c.ShouldBindJSON 做参数校验，缺字段直接返回 400
type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Login 处理 POST /api/v1/auth/login
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, http.StatusBadRequest, "参数错误：username 和 password 必填")
		return
	}

	user, err := service.Login(req.Username, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			common.Fail(c, http.StatusUnauthorized, err.Error())
			return
		}
		common.Fail(c, http.StatusInternalServerError, "服务器内部错误")
		return
	}

	common.Success(c, user)
}
