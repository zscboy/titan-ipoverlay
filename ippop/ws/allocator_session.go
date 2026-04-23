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

const defaultMaxErrorCount = 5

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
	maxErrorCount  int32
}

func NewSessionManager(source NodeSource, expire time.Duration) *SessionManager {
	sm := &SessionManager{
		sessions:       make(map[sessionKey]*UserSession),
		idleList:       list.New(),
		expireDuration: expire,
		source:         source,
		maxErrorCount:  defaultMaxErrorCount, // Default threshold
	}
	go sm.cleanerTask()
	return sm
}

func (sm *SessionManager) GetSession(username, sessionID string) *UserSession {
	sm.lock.RLock()
	defer sm.lock.RUnlock()
	return sm.sessions[sessionKey{username, sessionID}]
}

func (sm *SessionManager) SessionLen() int {
	sm.lock.RLock()
	defer sm.lock.RUnlock()
	return len(sm.sessions)
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

func (sm *SessionManager) Create(username, sessionID, deviceID, exitIP string, sessionExpireDuration time.Duration) *UserSession {
	if sessionExpireDuration == 0 {
		sessionExpireDuration = sm.expireDuration
	}

	key := sessionKey{username, sessionID}
	sess := &UserSession{
		username:  username,
		sessionID: sessionID,
		deviceID:  deviceID,
		exitIP:    exitIP,
		duration:  sessionExpireDuration,
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
		if sess.isEphemeral {
			var nodes []string
			var ips []string
			if sess.exitIP != "" {
				ips = append(ips, sess.exitIP)
			} else {
				nodes = append(nodes, sess.deviceID)
			}
			logx.Infof("SessionManager: immediate release ephemeral session for user %s, device %s", sess.username, sess.deviceID)
			sm.source.ReleaseExclusiveNodes(nodes, ips)
			return
		}

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
		// Use per-session duration for expiry
		if now.Sub(sess.disconnectAt) < sess.duration {
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
		exitIP, tun, err := a.source.AcquireExclusiveNode(context.Background())
		if err != nil {
			return nil, nil, err
		}
		sess := &UserSession{
			username:    user.UserName,
			deviceID:    tun.opts.Id,
			exitIP:      exitIP,
			isEphemeral: true,
		}
		atomic.StoreInt32(&sess.connectCount, 1)
		return tun, sess, nil
	}

	// Atomic Lookup and Activation.
	// Note: GetAndActivate increments connectCount if and only if a local tunnel is found.
	sess, tun := a.sm.GetAndActivate(user.UserName, target.Session)

	// Keep track if GetAndActivate already incremented the reference count.
	hasActiveRef := (tun != nil)

	if tun != nil {
		// If error count threshold reached, trigger re-allocation
		if sess.GetErrorCount() < a.sm.maxErrorCount {
			return tun, sess, nil
		}
		logx.Infof("SessionManager: session %s for user %s reached error threshold %d, triggering re-allocation", target.Session, user.UserName, sess.GetErrorCount())

		// Release old node/IP (mutually exclusive check)
		if len(sess.exitIP) > 0 {
			a.source.ReleaseExclusiveNodes(nil, []string{sess.exitIP})
		} else {
			a.source.ReleaseExclusiveNodes([]string{sess.deviceID}, nil)
		}

		// Clear current allocation to trigger new one
		a.sm.UpdateAllocation(sess, "", "")
	}

	// If we reach here, either sess is nil OR tun is dead (nil) OR it reached error threshold.
	// Allocate new exclusive node
	exitIP, tun, err := a.source.AcquireExclusiveNode(context.Background())
	if err != nil {
		// If we had an active reference from GetAndActivate, we must release it
		if hasActiveRef {
			a.sm.Decrement(sess)
		}
		return nil, nil, err
	}

	if sess == nil {
		sess = a.sm.Create(user.UserName, target.Session, tun.opts.Id, exitIP, target.SessTime)
		a.sm.Activate(sess) // Set count to 1
	} else {
		// Update existing session with new device node and IP
		a.sm.UpdateAllocation(sess, tun.opts.Id, exitIP)
		// Reset error count on successful re-allocation
		sess.ResetError()

		// If we didn't have an active reference yet (because GetAndActivate returned nil tun), activate it now
		if !hasActiveRef {
			a.sm.Activate(sess)
		}
	}

	return tun, sess, nil
}
