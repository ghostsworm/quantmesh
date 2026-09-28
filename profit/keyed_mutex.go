package profit

import "sync"

type keyedLockEntry struct {
	mu   sync.Mutex
	refs int
}

type keyedMutex struct {
	mu    sync.Mutex
	locks map[string]*keyedLockEntry
}

func (k *keyedMutex) lock(key string) func() {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = make(map[string]*keyedLockEntry)
	}
	entry := k.locks[key]
	if entry == nil {
		entry = &keyedLockEntry{}
		k.locks[key] = entry
	}
	entry.refs++
	k.mu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		k.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(k.locks, key)
		}
		k.mu.Unlock()
	}
}

var withdrawRuleLocks keyedMutex
