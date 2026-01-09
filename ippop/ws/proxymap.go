package ws

import (
	"sync"
	"sync/atomic"
)

// ProxyMap is a thread-safe map that maintains an atomic count of its elements.
// It wraps sync.Map to provide efficient session tracking.
type ProxyMap struct {
	m     sync.Map
	count atomic.Int32
}

// Store sets the value for a key.
// Returns true if the key was newly added to the map (count incremented).
// Returns false if the key was already present (value updated, count unchanged).
func (pm *ProxyMap) Store(key, value any) bool {
	_, loaded := pm.m.LoadOrStore(key, value)
	if !loaded {
		pm.count.Add(1)
		return true
	}
	// If already exists, overwrite with new value
	pm.m.Store(key, value)
	return false
}

// Load returns the value stored in the map for a key, or nil if no value is present.
// The ok result indicates whether value was found in the map.
func (pm *ProxyMap) Load(key any) (value any, ok bool) {
	return pm.m.Load(key)
}

// Delete removes the value for a key.
// Returns true if the key was present and removed (count decremented).
func (pm *ProxyMap) Delete(key any) bool {
	_, loaded := pm.m.LoadAndDelete(key)
	if loaded {
		pm.count.Add(-1)
		return true
	}
	return false
}

// LoadAndDelete deletes the value for a key, returning the previous value if any.
// The loaded result reports whether the key was present.
func (pm *ProxyMap) LoadAndDelete(key any) (value any, loaded bool) {
	value, loaded = pm.m.LoadAndDelete(key)
	if loaded {
		pm.count.Add(-1)
	}
	return
}

// Count returns the current number of elements in the map.
func (pm *ProxyMap) Count() int32 {
	return pm.count.Load()
}

// Range calls f sequentially for each key and value present in the map.
// If f returns false, range stops the iteration.
func (pm *ProxyMap) Range(f func(key, value any) bool) {
	pm.m.Range(f)
}

// Clear removes all elements from the map and resets the count.
func (pm *ProxyMap) Clear() {
	pm.m.Clear()
	pm.count.Store(0)
}
