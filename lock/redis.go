package lock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisLock Redis 分布式锁实现
type RedisLock struct {
	client   *redis.Client
	prefix   string
	lockID   string            // 当前实例的唯一标识
	lockKeys map[string]string // 记录持有的锁和对应的 token
}

// NewRedisLock 创建 Redis 分布式锁
func NewRedisLock(client *redis.Client, prefix string) *RedisLock {
	return &RedisLock{
		client:   client,
		prefix:   prefix,
		lockID:   generateLockID(),
		lockKeys: make(map[string]string),
	}
}

// generateLockID 生成唯一的锁 ID
func generateLockID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// generateToken 为每个锁生成唯一的 token
func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Lock 获取锁，阻塞直到成功或超时
func (r *RedisLock) Lock(ctx context.Context, key string, ttl time.Duration) error {
	lockKey := r.prefix + key
	token := generateToken()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			ok, err := r.client.SetNX(ctx, lockKey, token, ttl).Result()
			if err != nil {
				return fmt.Errorf("redis setnx failed: %w", err)
			}
			if ok {
				r.lockKeys[key] = token
				return nil
			}
		}
	}
}

// TryLock 尝试获取锁，立即返回
func (r *RedisLock) TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	lockKey := r.prefix + key
	token := generateToken()

	ok, err := r.client.SetNX(ctx, lockKey, token, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("redis setnx failed: %w", err)
	}

	if ok {
		r.lockKeys[key] = token
	}

	return ok, nil
}

// Unlock 释放锁
func (r *RedisLock) Unlock(ctx context.Context, key string) error {
	lockKey := r.prefix + key
	token, exists := r.lockKeys[key]
	if !exists {
		return fmt.Errorf("lock not held: %s", key)
	}

	// Lua 脚本确保原子性：只有持有锁的实例才能释放
	script := `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("del", KEYS[1])
		else
			return 0
		end
	`

	result, err := r.client.Eval(ctx, script, []string{lockKey}, token).Result()
	if err != nil {
		return fmt.Errorf("redis eval failed: %w", err)
	}

	if result.(int64) == 0 {
		return fmt.Errorf("lock not held or expired: %s", key)
	}

	delete(r.lockKeys, key)
	return nil
}

// Extend 延长锁的过期时间
func (r *RedisLock) Extend(ctx context.Context, key string, ttl time.Duration) error {
	lockKey := r.prefix + key
	token, exists := r.lockKeys[key]
	if !exists {
		return fmt.Errorf("lock not held: %s", key)
	}

	// Lua 脚本确保原子性：只有持有锁的实例才能延期
	script := `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("expire", KEYS[1], ARGV[2])
		else
			return 0
		end
	`

	result, err := r.client.Eval(ctx, script, []string{lockKey}, token, int(ttl.Seconds())).Result()
	if err != nil {
		return fmt.Errorf("redis eval failed: %w", err)
	}

	if result.(int64) == 0 {
		return fmt.Errorf("lock not held or expired: %s", key)
	}

	return nil
}

// Close 关闭连接
func (r *RedisLock) Close() error {
	return r.client.Close()
}

// Ping 检查连接
func (r *RedisLock) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}
