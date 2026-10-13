package model

import "time"

// User 对应数据库里的 users 表（GORM 默认把结构体名复数作为表名）
// gorm tag 决定建表/查询时的列属性，json tag 决定接口输出的字段名
type User struct {
	ID        uint      `gorm:"primaryKey" json:"id"`                      // 主键，自增
	Username  string    `gorm:"size:64;uniqueIndex" json:"username"`       // 用户名，唯一索引
	Password  string    `gorm:"size:128" json:"-"`                         // bcrypt 哈希；json:"-" 表示序列化时隐藏
	Nickname  string    `gorm:"size:64" json:"nickname"`                   // 昵称
	CreatedAt time.Time `json:"created_at"`                                // GORM 自动维护
	UpdatedAt time.Time `json:"updated_at"`                                // GORM 自动维护
}
