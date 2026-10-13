package dao

import (
	"GoLangStudy/database"
	"GoLangStudy/model"
)

// GetUserByUsername 按用户名查用户
// 找不到时返回 gorm.ErrRecordNotFound，原样上抛，
// 由 service 层翻译成业务错误（dao 不关心业务语义）
func GetUserByUsername(username string) (*model.User, error) {
	var user model.User
	err := database.DB.Where("username = ?", username).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}
