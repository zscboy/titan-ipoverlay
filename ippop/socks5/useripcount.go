package socks5

import "sync"

type userIPCount struct {
	userIP map[string]int
	lock   sync.Mutex
}

func newUserIPCount() *userIPCount {
	return &userIPCount{userIP: make(map[string]int)}
}

func (count *userIPCount) incr(key string) int {
	count.lock.Lock()
	defer count.lock.Unlock()

	count.userIP[key] = count.userIP[key] + 1
	return count.userIP[key]
}

func (count *userIPCount) decr(key string) int {
	count.lock.Lock()
	defer count.lock.Unlock()

	v, ok := count.userIP[key]
	if ok {
		v = v - 1
	}

	if v <= 0 {
		delete(count.userIP, key)
	} else {
		count.userIP[key] = v
	}
	return v
}

func (count *userIPCount) get(key string) int {
	count.lock.Lock()
	defer count.lock.Unlock()

	return count.userIP[key]
}
