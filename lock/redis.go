package lock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisClient RedisLock 依賴的最小 Redis 命令集（便於測試替換）
type redisClient interface {
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd
	Eval(ctx context.Context, script string, keys []string, args ...interface{}) *redis.Cmd
	Ping(ctx context.Context) *redis.StatusCmd
	Close() error
}

// RedisLock Redis 分布式鎖實現
type RedisLock struct {
	client    redisClient
	prefix    string
	lockID    string            // 當前實例的唯一標识
	mu        sync.Mutex        // 保護 lockKeys（多槽位並發下單會同時讀寫）
	lockKeys  map[string]string // 記錄持有的鎖和對应的 token
	acquiring map[string]struct{}
}

// NewRedisLock 創建 Redis 分布式鎖
func NewRedisLock(client *redis.Client, prefix string) *RedisLock {
	return newRedisLock(client, prefix)
}

func newRedisLock(client redisClient, prefix string) *RedisLock {
	return &RedisLock{
		client:    client,
		prefix:    prefix,
		lockID:    generateLockID(),
		lockKeys:  make(map[string]string),
		acquiring: make(map[string]struct{}),
	}
}

func (r *RedisLock) getToken(key string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	token, ok := r.lockKeys[key]
	return token, ok
}

// deleteToken 僅在 token 未被重新獲取覆蓋時刪除
func (r *RedisLock) deleteToken(key, token string) {
	r.mu.Lock()
	if r.lockKeys[key] == token {
		delete(r.lockKeys, key)
	}
	r.mu.Unlock()
}

// tryLockOnce reserves the local key before contacting Redis. A local token
// remains reserved until Unlock completes, even if its Redis TTL has expired;
// otherwise a stale Unlock(key) could read and release a newer token acquired
// by this same RedisLock instance.
func (r *RedisLock) tryLockOnce(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	r.mu.Lock()
	if _, held := r.lockKeys[key]; held {
		r.mu.Unlock()
		return false, nil
	}
	if _, acquiring := r.acquiring[key]; acquiring {
		r.mu.Unlock()
		return false, nil
	}
	r.acquiring[key] = struct{}{}
	r.mu.Unlock()

	token := generateToken()
	ok, err := r.client.SetNX(ctx, r.prefix+key, token, ttl).Result()
	r.mu.Lock()
	delete(r.acquiring, key)
	if err == nil && ok {
		r.lockKeys[key] = token
	}
	r.mu.Unlock()
	if err != nil {
		return false, fmt.Errorf("redis setnx failed: %w", err)
	}
	return ok, nil
}

// generateLockID 生成唯一的鎖 ID
func generateLockID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// generateToken 為每個鎖生成唯一的 token
func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Lock 獲取鎖，阻塞直到成功或超時
func (r *RedisLock) Lock(ctx context.Context, key string, ttl time.Duration) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			ok, err := r.tryLockOnce(ctx, key, ttl)
			if err != nil {
				return err
			}
			if ok {
				return nil
			}
		}
	}
}

// TryLock 尝試獲取鎖，立即返回
func (r *RedisLock) TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return r.tryLockOnce(ctx, key, ttl)
}

// Unlock 释放鎖
func (r *RedisLock) Unlock(ctx context.Context, key string) error {
	lockKey := r.prefix + key
	token, exists := r.getToken(key)
	if !exists {
		return fmt.Errorf("lock not held: %s", key)
	}

	// Lua 脚本确保原子性：只有持有鎖的實例才能释放
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

	if n, ok := result.(int64); !ok || n == 0 {
		// 鎖已過期或被他人持有：本地記錄已無意義，一併清除
		r.deleteToken(key, token)
		return fmt.Errorf("lock not held or expired: %s", key)
	}

	r.deleteToken(key, token)
	return nil
}

// Extend 延长鎖的過期時间
func (r *RedisLock) Extend(ctx context.Context, key string, ttl time.Duration) error {
	lockKey := r.prefix + key
	token, exists := r.getToken(key)
	if !exists {
		return fmt.Errorf("lock not held: %s", key)
	}

	// Lua 脚本确保原子性：只有持有鎖的實例才能延期（毫秒精度，避免 <1s TTL 被截斷為 0）
	script := `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("pexpire", KEYS[1], ARGV[2])
		else
			return 0
		end
	`

	result, err := r.client.Eval(ctx, script, []string{lockKey}, token, ttl.Milliseconds()).Result()
	if err != nil {
		return fmt.Errorf("redis eval failed: %w", err)
	}

	if n, ok := result.(int64); !ok || n == 0 {
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
