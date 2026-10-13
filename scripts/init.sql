-- 初始化脚本：建库、建表、插入测试用户
-- 执行方式（注意 --default-character-set=utf8mb4，否则中文会乱码）：
--   mysql -uroot -p --default-character-set=utf8mb4 < scripts/init.sql

CREATE DATABASE IF NOT EXISTS golang_study DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

USE golang_study;

CREATE TABLE IF NOT EXISTS users (
    id         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    username   VARCHAR(64)  NOT NULL,
    password   VARCHAR(128) NOT NULL COMMENT 'bcrypt 哈希',
    nickname   VARCHAR(64)  NOT NULL DEFAULT '',
    created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    UNIQUE INDEX idx_username (username)
) ENGINE = InnoDB COMMENT '用户表';

-- 测试用户：admin / 123456
-- 密码哈希由 go run ./cmd/genhash 123456 生成
INSERT INTO users (username, password, nickname)
VALUES ('admin', '$2a$10$PB1X7gK5PrqG4bSxDPDz1eM3QoK0yXPpzApiZgXtOsO.J5t3wxFYq', '管理员')
ON DUPLICATE KEY UPDATE password = VALUES(password), nickname = VALUES(nickname);
