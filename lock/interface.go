package lock

import (
	"context"
	"time"
)

// DistributedLock 分布式锁接口
type DistributedLock interface {
	// Lock 获取锁，阻塞直到成功或超时
	Lock(ctx context.Context, key string, ttl time.Duration) error

	// TryLock 尝试获取锁，立即返回
	// 返回 true 表示成功获取锁，false 表示锁已被占用
	TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error)

	// Unlock 释放锁
	Unlock(ctx context.Context, key string) error

	// Extend 延长锁的过期时间
	Extend(ctx context.Context, key string, ttl time.Duration) error

	// Close 关闭连接
	Close() error
}

// NopLock 空实现（单实例模式）
type NopLock struct{}

func NewNopLock() *NopLock {
	return &NopLock{}
}

func (n *NopLock) Lock(ctx context.Context, key string, ttl time.Duration) error {
	return nil
}

func (n *NopLock) TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return true, nil
}

func (n *NopLock) Unlock(ctx context.Context, key string) error {
	return nil
}

func (n *NopLock) Extend(ctx context.Context, key string, ttl time.Duration) error {
	return nil
}

func (n *NopLock) Close() error {
	return nil
}



