package ws

import (
	"math/rand"
	"sync"
)

type lockedSource struct {
	lk sync.Mutex
	s  rand.Source
}

func (r *lockedSource) Int63() (n int64) {
	r.lk.Lock()
	n = r.s.Int63()
	r.lk.Unlock()
	return
}

func (r *lockedSource) Seed(seed int64) {
	r.lk.Lock()
	r.s.Seed(seed)
	r.lk.Unlock()
}
