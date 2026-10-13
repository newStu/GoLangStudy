package service

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"GoLangStudy/dao"
	"GoLangStudy/model"
)

// ErrInvalidCredentials 用户名或密码错误
// 安全要点：用户不存在和密码错误对外返回同一个错误，
// 避免攻击者通过不同的报错信息探测哪些用户名存在
var ErrInvalidCredentials = errors.New("用户名或密码错误")

// Login 登录业务逻辑：查用户 → 比对密码
func Login(username, password string) (*model.User, error) {
	user, err := dao.GetUserByUsername(username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidCredentials // 用户不存在，和密码错误同样处理
		}
		return nil, err // 其他错误（数据库挂了等）原样上抛，handler 按 500 处理
	}

	// bcrypt 比对：数据库存的是哈希，明文密码只在这里短暂出现
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	return user, nil
}
