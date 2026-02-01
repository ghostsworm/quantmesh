package database

import (
	"fmt"
	"time"
)

// Config 數據库配置
type Config struct {
	Type            string
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	LogLevel        string
}

// NewDatabase 根據配置創建數據库實例
func NewDatabase(config *Config) (Database, error) {
	dbConfig := &DBConfig{
		Type:            config.Type,
		DSN:             config.DSN,
		MaxOpenConns:    config.MaxOpenConns,
		MaxIdleConns:    config.MaxIdleConns,
		ConnMaxLifetime: config.ConnMaxLifetime,
		LogLevel:        config.LogLevel,
	}

	switch config.Type {
	case "sqlite", "postgres", "postgresql", "mysql":
		return NewGormDatabase(dbConfig)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", config.Type)
	}
}
