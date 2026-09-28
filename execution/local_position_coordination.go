package execution

import (
	"context"
	"errors"
	"sync"
)

type localPositionLockEntry struct {
	semaphore chan struct{}
	users     int
}

var localPositionLocks = struct {
	sync.Mutex
	entries map[string]*localPositionLockEntry
}{entries: make(map[string]*localPositionLockEntry)}

// AcquireLocalPositionCoordination serializes same-process order submissions
// and position snapshots even when distributed locking is disabled. Multi-
// process deployments must also acquire the distributed lock for this key.
func AcquireLocalPositionCoordination(ctx context.Context, key string) (func(), error) {
	if ctx == nil {
		return nil, errors.New("position coordination context is nil")
	}
	if key == "" {
		return nil, errors.New("position coordination key is empty")
	}

	localPositionLocks.Lock()
	entry := localPositionLocks.entries[key]
	if entry == nil {
		entry = &localPositionLockEntry{semaphore: make(chan struct{}, 1)}
		localPositionLocks.entries[key] = entry
	}
	entry.users++
	localPositionLocks.Unlock()

	select {
	case entry.semaphore <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() {
				<-entry.semaphore
				localPositionLocks.Lock()
				entry.users--
				if entry.users == 0 && localPositionLocks.entries[key] == entry {
					delete(localPositionLocks.entries, key)
				}
				localPositionLocks.Unlock()
			})
		}, nil
	case <-ctx.Done():
		localPositionLocks.Lock()
		entry.users--
		if entry.users == 0 && localPositionLocks.entries[key] == entry {
			delete(localPositionLocks.entries, key)
		}
		localPositionLocks.Unlock()
		return nil, ctx.Err()
	}
}
