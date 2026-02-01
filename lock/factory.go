package lock

import (
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Config 分布式鎖配置
type Config struct {
	Enabled    bool
	Type       string
	Prefix     string
	DefaultTTL time.Duration
	Redis      RedisConfig
}

// RedisConfig Redis 配置
type RedisConfig struct {
	Addr     string
	Password string
	DB       int
	PoolSize int
}

// NewDistributedLock 根據配置創建分布式鎖實例
// 如果未啟用分布式鎖，回傳 NopLock（零开销）
func NewDistributedLock(config *Config) (DistributedLock, error) {
	// 如果未啟用，返回空實現（單實例模式）
	if !config.Enabled {
		return NewNopLock(), nil
	}

	switch config.Type {
	case "redis":
		// 創建 Redis 客戶端
		client := redis.NewClient(&redis.Options{
			Addr:     config.Redis.Addr,
			Password: config.Redis.Password,
			DB:       config.Redis.DB,
			PoolSize: config.Redis.PoolSize,
		})

		return NewRedisLock(client, config.Prefix), nil

	case "etcd":
		// TODO: 實現 etcd 分布式鎖
		return nil, fmt.Errorf("etcd lock not implemented yet")

	case "database":
		// TODO: 實現數據库分布式鎖
		return nil, fmt.Errorf("database lock not implemented yet")

	default:
		return nil, fmt.Errorf("unsupported lock type: %s", config.Type)
	}
}
