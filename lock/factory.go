package lock

import (
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Config 分布式锁配置
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

// NewDistributedLock 根据配置创建分布式锁实例
// 如果未启用分布式锁，返回 NopLock（零开销）
func NewDistributedLock(config *Config) (DistributedLock, error) {
	// 如果未启用，返回空实现（单实例模式）
	if !config.Enabled {
		return NewNopLock(), nil
	}

	switch config.Type {
	case "redis":
		// 创建 Redis 客户端
		client := redis.NewClient(&redis.Options{
			Addr:     config.Redis.Addr,
			Password: config.Redis.Password,
			DB:       config.Redis.DB,
			PoolSize: config.Redis.PoolSize,
		})

		return NewRedisLock(client, config.Prefix), nil

	case "etcd":
		// TODO: 实现 etcd 分布式锁
		return nil, fmt.Errorf("etcd lock not implemented yet")

	case "database":
		// TODO: 实现数据库分布式锁
		return nil, fmt.Errorf("database lock not implemented yet")

	default:
		return nil, fmt.Errorf("unsupported lock type: %s", config.Type)
	}
}
