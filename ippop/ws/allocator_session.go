package ws

import (
	"container/list"
	"context"
	"sync"
	"sync/atomic"
	"time"
	"titan-ipoverlay/ippop/model"
	"titan-ipoverlay/ippop/socks5"

	"github.com/zeromicro/go-zero/core/logx"
)

type sessionKey struct {
	username  string
	sessionID string
}

type SessionManager struct {
	lock           sync.RWMutex
	sessions       map[sessionKey]*UserSession
	idleList       *list.List
	expireDuration time.Duration
	source         NodeSource
}

func NewSessionManager(source NodeSource, expire time.Duration) *SessionManager {
	sm := &SessionManager{
		sessions:       make(map[sessionKey]*UserSession),
		idleList:       list.New(),
		expireDuration: expire,
		source:         source,
	}
	go sm.cleanerTask()
	return sm
}

func (sm *SessionManager) GetSession(username, sessionID string) *UserSession {
	sm.lock.RLock()
	defer sm.lock.RUnlock()
	return sm.sessions[sessionKey{username, sessionID}]
}

func (sm *SessionManager) GetAndActivate(username, sessionID string) (*UserSession, *Tunnel) {
	key := sessionKey{username, sessionID}
	sm.lock.Lock()
	defer sm.lock.Unlock()

	sess, ok := sm.sessions[key]
	if !ok {
		return nil, nil
	}

	// Double check tunnel status inside lock
	tun := sm.source.GetLocalTunnel(sess.deviceID)
	if tun == nil {
		return sess, nil
	}

	// Increment ref count and remove from idle list atomically
	atomic.AddInt32(&sess.connectCount, 1)
	if sess.idleElement != nil {
		sm.idleList.Remove(sess.idleElement)
		sess.idleElement = nil
	}

	return sess, tun
}

// UpdateAllocation safely updates the deviceID and exitIP of a session
func (sm *SessionManager) UpdateAllocation(sess *UserSession, deviceID, exitIP string) {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	sess.deviceID = deviceID
	sess.exitIP = exitIP
}

func (sm *SessionManager) Create(username, sessionID, deviceID, exitIP string) *UserSession {
	key := sessionKey{username, sessionID}
	sess := &UserSession{
		username:  username,
		sessionID: sessionID,
		deviceID:  deviceID,
		exitIP:    exitIP,
	}
	sm.lock.Lock()
	sm.sessions[key] = sess
	sm.lock.Unlock()
	return sess
}

func (sm *SessionManager) Decrement(sess *UserSession) {
	if sess == nil {
		return
	}

	// 1. Atomic decrement reference count
	newCount := atomic.AddInt32(&sess.connectCount, -1)

	// 2. If count reaches zero, enter delayed cleanup queue
	if newCount <= 0 {
		sm.lock.Lock()
		defer sm.lock.Unlock()

		// Double check to prevent race conditions
		if atomic.LoadInt32(&sess.connectCount) <= 0 {
			if sess.idleElement == nil {
				sess.disconnectAt = time.Now()
				sess.idleElement = sm.idleList.PushBack(sess)
			} else {
				// Should not happen in theory, but for robustness:
				sm.idleList.MoveToBack(sess.idleElement)
			}
		}
	}
}

// OnSessionIdle and OnSessionActive are now internal or replaced by Decrement.
// onSessionActive is now internal part of GetAndActivate and Activate.
func (sm *SessionManager) Activate(sess *UserSession) {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	atomic.AddInt32(&sess.connectCount, 1)
	if sess.idleElement != nil {
		sm.idleList.Remove(sess.idleElement)
		sess.idleElement = nil
	}
}

func (sm *SessionManager) cleanerTask() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		sm.checkAndClean()
	}
}

func (sm *SessionManager) checkAndClean() {
	sm.lock.Lock()
	defer sm.lock.Unlock()

	now := time.Now()
	var nodesToRelease []string
	var ipsToRelease []string
	for {
		element := sm.idleList.Front()
		if element == nil {
			break
		}

		sess := element.Value.(*UserSession)
		// Check if expired
		if now.Sub(sess.disconnectAt) < sm.expireDuration {
			// Since it's ordered by time, if head is not expired, none is.
			break
		}

		// Double check connectCount
		if atomic.LoadInt32(&sess.connectCount) == 0 {
			// Perform cleanup
			logx.Infof("SessionManager: cleaning up expired session %s for user %s, device %s", sess.sessionID, sess.username, sess.deviceID)
			delete(sm.sessions, sessionKey{sess.username, sess.sessionID})
			sm.idleList.Remove(element)
			sess.idleElement = nil

			// Collect node IDs or IPs for batch release
			if sess.exitIP != "" {
				ipsToRelease = append(ipsToRelease, sess.exitIP)
			} else {
				nodesToRelease = append(nodesToRelease, sess.deviceID)
			}
		} else {
			// Should have been removed by OnSessionActive, but for safety:
			sm.idleList.Remove(element)
			sess.idleElement = nil
		}
	}

	if len(nodesToRelease) > 0 || len(ipsToRelease) > 0 {
		sm.source.ReleaseExclusiveNodes(nodesToRelease, ipsToRelease)
	}
}

// SessionAllocator implements NodeAllocator for Custom mode.
type SessionAllocator struct {
	sm     *SessionManager
	source NodeSource
}

func NewSessionAllocator(sm *SessionManager, source NodeSource) *SessionAllocator {
	return &SessionAllocator{sm: sm, source: source}
}
func (a *SessionAllocator) Allocate(user *model.User, target *socks5.SocksTargetInfo) (*Tunnel, *UserSession, error) {
	if target.Session == "" {
		tun, err := a.source.PickActiveTunnel()
		return tun, nil, err
	}

	// Atomic Lookup and Activation
	sess, tun := a.sm.GetAndActivate(user.UserName, target.Session)
	if tun != nil {
		return tun, sess, nil
	}

	// If we reach here, either sess is nil OR tun is dead.
	// Allocate new exclusive node
	exitIP, tun, err := a.source.AcquireExclusiveNode(context.Background())
	if err != nil {
		return nil, nil, err
	}

	if sess == nil {
		sess = a.sm.Create(user.UserName, target.Session, tun.opts.Id, exitIP)
	} else {
		// Update existing session with new device node and IP
		a.sm.UpdateAllocation(sess, tun.opts.Id, exitIP)
	}

	a.sm.Activate(sess)

	return tun, sess, nil
}
