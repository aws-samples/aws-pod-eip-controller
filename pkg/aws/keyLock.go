package aws

import "sync"

type KeyLock struct {
	locks    sync.Map
	syncLock sync.Mutex
}

func NewKeyLock() *KeyLock {
	return &KeyLock{}
}

func (kl *KeyLock) Lock(key string) {
	lock, ok := kl.locks.Load(key)
	if !ok {
		kl.syncLock.Lock()
		defer kl.syncLock.Unlock()
		lock, ok = kl.locks.Load(key)
		if ok {
			lock.(*sync.Mutex).Lock()
			return
		}
		lock = &sync.Mutex{}
		kl.locks.Store(key, lock)
	}
	lock.(*sync.Mutex).Lock()
}

func (kl *KeyLock) Unlock(key string) {
	lock, ok := kl.locks.Load(key)
	if !ok {
		return
	}
	lock.(*sync.Mutex).Unlock()
}
