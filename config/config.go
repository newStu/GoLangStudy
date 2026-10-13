package config

// MySQLDSN 是 MySQL 连接串（Data Source Name）
// 格式：用户名:密码@tcp(地址:端口)/库名?参数
// 学习阶段先用常量写死；实际项目通常放配置文件或环境变量，避免密码进代码库
// 注意：这里用的是专用账号 golang（只有 golang_study 库的权限），
// 实际项目也不该用 root 直连业务库
const MySQLDSN = "golang:golang123456@tcp(127.0.0.1:3306)/golang_study?charset=utf8mb4&parseTime=True&loc=Local"
