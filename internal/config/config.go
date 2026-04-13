// Package config 提供服务配置管理
package config

import (
	"os"
	"strconv"
)

// Config 全局配置
type Config struct {
	// SSH 隧道配置
	SSHHost    string
	SSHUser    string
	SSHKeyPath string
	SSHPort    int

	// 数据库配置
	DBHost     string
	DBPort     int
	DBUser     string
	DBPassword string
	LocalPort  int

	// HTTP 服务配置
	ServerPort int
	ServerHost string

	// 匹配配置
	RunPlayers bool
}

// Default 返回默认配置（从环境变量读取，有默认值）
func Default() *Config {
	return &Config{
		SSHHost:    getEnv("SSH_HOST", "54.69.237.139"),
		SSHUser:    getEnv("SSH_USER", "ubuntu"),
		SSHKeyPath: getEnv("SSH_KEY_PATH", "/home/ubuntu/skills/xp-bet-db-connector/templates/id_ed25519"),
		SSHPort:    getEnvInt("SSH_PORT", 22),

		DBHost:     getEnv("DB_HOST", "test-db.cluster-cdgqiwig2x00.us-west-2.rds.amazonaws.com"),
		DBPort:     getEnvInt("DB_PORT", 3306),
		DBUser:     getEnv("DB_USER", "root"),
		DBPassword: getEnv("DB_PASSWORD", "r74pqyYtgdjlYB41jmWA"),
		LocalPort:  getEnvInt("LOCAL_PORT", 13400),

		ServerPort: getEnvInt("SERVER_PORT", 8080),
		ServerHost: getEnv("SERVER_HOST", "0.0.0.0"),

		RunPlayers: getEnvBool("RUN_PLAYERS", true),
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return defaultVal
}
