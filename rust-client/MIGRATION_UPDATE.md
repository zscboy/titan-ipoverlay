# Rust Client Migration Update

**Date**: 2025-12-27  
**Purpose**: Updated Rust client to match the latest fixed Go client code

## Summary of Changes

This update synchronizes the Rust client implementation with the latest Go client fixes, including:

1. **Removed double async spawn** - matching the Go fix where redundant goroutine was removed
2. **Fixed connection failure handling** - proper signaling to prevent resource leaks
3. **Replaced all Chinese text with English** - for better internationalization
4. **Added error categorization** - distinguish between NODE_UNSTABLE and NODE_TIMEOUT

---

## Detailed Changes

### 1. `src/tunnel/mod.rs`

#### Change 1.1: Removed Double Async Spawn (Lines 182-189)

**Go Fix Reference**: Removed `go` keyword in `onProxySessionCreate`

**Before**:
```rust
pb::MessageType::ProxySessionCreate => {
    let sid = msg.session_id.clone();
    let payload = msg.payload.clone();
    let self_clone = Arc::new(self.clone_for_task(write_tx));
    tokio::spawn(async move {  // ← Redundant spawn
        if let Err(e) = self_clone.create_proxy_session(sid, payload).await {
            error!("create_proxy_session error: {}", e);
        }
    });
}
```

**After**:
```rust
pb::MessageType::ProxySessionCreate => {
    let self_clone = Arc::new(self.clone_for_task(write_tx));
    if let Err(e) = self_clone.create_proxy_session(msg.session_id.clone(), msg.payload.clone()).await {
        error!("create_proxy_session error: {}", e);
    }
}
```

**Rationale**: Removed unnecessary async task spawn since `create_proxy_session` already spawns its own task internally. This matches the Go fix.

---

#### Change 1.2: Connection Error Handling & English Translation (Lines 267-295)

**Go Fix Reference**: 
- Line 303-308: Added error categorization
- Line 312: Close `connected` channel on failure
- Replaced Chinese error messages

**Before**:
```rust
Ok(Err(e)) => {
    error!("dial {} failed: {}", dest_addr.addr, e);
    let _ = self_clone.create_proxy_session_reply(&sid_reply, Some(e.to_string())).await;
    self_clone.on_proxy_conn_close(&sid_reply).await;
}
Err(_) => {
    error!("[NODE_时延大/超时] dial {} failed: timeout", dest_addr.addr);  // ← Chinese
    let _ = self_clone.create_proxy_session_reply(&sid_reply, Some("timeout".to_string())).await;
    self_clone.on_proxy_conn_close(&sid_reply).await;
}
```

**After**:
```rust
Ok(Err(e)) => {
    // Categorize dial error for diagnostics
    let error_tag = if e.kind() == std::io::ErrorKind::TimedOut {
        "[NODE_TIMEOUT]"
    } else {
        "[NODE_UNSTABLE]"
    };
    error!("{} dial {} failed: {}", error_tag, dest_addr.addr, e);
    let _ = self_clone.create_proxy_session_reply(&sid_reply, Some(e.to_string())).await;
    self_clone.on_proxy_conn_close(&sid_reply).await;
    // Signal connection failure to proxy
    proxy.signal_connection_failed().await;  // ← NEW: Prevent resource leak
}
Err(_) => {
    error!("[NODE_TIMEOUT] dial {} failed: timeout", dest_addr.addr);  // ← English
    let _ = self_clone.create_proxy_session_reply(&sid_reply, Some("timeout".to_string())).await;
    self_clone.on_proxy_conn_close(&sid_reply).await;
    // Signal connection failure to proxy
    proxy.signal_connection_failed().await;  // ← NEW: Prevent resource leak
}
```

**Rationale**:
- **Error Categorization**: Distinguish between timeout errors and other connection failures
- **Chinese → English**: Replaced `NODE_时延大/超时` with `[NODE_TIMEOUT]` and `[NODE_不稳定]` with `[NODE_UNSTABLE]`
- **Resource Leak Fix**: Call `signal_connection_failed()` to set `conn = None`, preventing write loop from waiting indefinitely

---

### 2. `src/tunnel/tcpproxy.rs`

#### Change 2.1: Added Connection Failure Signaling (Lines 53-58)

**Go Fix Reference**: Line 312 in `tunnel.go` - `close(proxySession.connected)`

**Added Method**:
```rust
/// Signal that connection has failed - sets conn to None to unblock waiting writers
pub async fn signal_connection_failed(&self) {
    let mut guard = self.conn.lock().await;
    *guard = None;
}
```

**Rationale**: In Go, we `close(proxySession.connected)` to unblock goroutines. In Rust, we set `conn = None` to signal failure to the write loop.

---

#### Change 2.2: Improved Write Loop Logic (Lines 24-38)

**Go Fix Reference**: `tcpproxy.go` Line 65 - `if proxy.conn == nil { return }`

**Before**:
```rust
} else {
    // Wait for connection
    drop(guard);
    tokio::time::sleep(tokio::time::Duration::from_millis(10)).await;  // ← Busy wait
}
```

**After**:
```rust
} else {
    // Connection is None (either not set yet or failed)
    error!("TCPProxy {} conn is None, exiting write loop", id_clone);
    return;  // ← Exit immediately
}
```

**Rationale**: 
- Go version immediately returns when `conn == nil`
- Rust version now exits immediately instead of sleeping and retrying
- This prevents resource leaks when connection fails

---

## Mapping: Go → Rust Concepts

| Go Concept | Rust Equivalent | Purpose |
|------------|----------------|---------|
| `go func()` | `tokio::spawn(async move {})` | Spawn async task |
| `close(channel)` | Set `Option<T>` to `None` | Signal completion/failure |
| `<-channel` blocking | `Option` check in loop | Wait for initialization |
| `sync.Map` | `Arc<Mutex<HashMap>>` | Thread-safe map |
| `goroutine leak` | Task/resource leak | Failed to clean up async tasks |

---

## Critical Fixes Applied

### 🔴 **Critical**: Goroutine/Task Leak Prevention

**Problem**: When TCP connection failed in Go, the `connected` channel was never closed, causing `startWriteLoop` to block forever.

**Go Fix**: 
```go
close(proxySession.connected)  // Line 312
proxySession.conn = nil
```

**Rust Equivalent**:
```rust
proxy.signal_connection_failed().await;  // Sets conn = None
```

**Impact**: Without this fix, every failed connection would leak a task/goroutine that waits forever.

---

### ⚠️ **Important**: Error Categorization

**Before**: Generic error messages or Chinese text  
**After**: Structured error tags:
- `[NODE_UNSTABLE]` - General connection failures (DNS, refused, etc.)
- `[NODE_TIMEOUT]` - Timeout-specific failures

**Benefit**: Makes log analysis and debugging much easier

---

## Testing Recommendations

1. **Test Connection Failures**:
   ```bash
   # Test with unreachable destination
   # Verify no task leaks occur
   # Check logs for proper error categorization
   ```

2. **Monitor Resource Usage**:
   - Check that failed connections don't accumulate tasks
   - Verify memory doesn't grow over time with failed connections

3. **Log Verification**:
   - Confirm all logs are in English
   - Verify `[NODE_UNSTABLE]` and `[NODE_TIMEOUT]` tags appear correctly

---

## Cargo/Rustc Version Note

**Issue**: Current Cargo version (1.76.0) is too old to compile dependencies requiring `edition2024`

**Solution**: Update Rust toolchain:
```bash
rustup update stable
# or
rustup default nightly
```

**Minimum Required**: Cargo 1.80+ recommended

---

## Files Modified

1. `src/tunnel/mod.rs` - Main tunnel logic
2. `src/tunnel/tcpproxy.rs` - TCP proxy implementation

## Git Commit Message

```
fix: sync Rust client with latest Go client fixes

- Remove double async spawn to match Go goroutine fix
- Fix critical task leak: signal connection failure properly
- Replace all Chinese text with English for i18n
- Add error categorization: NODE_UNSTABLE vs NODE_TIMEOUT
- Improve write loop to exit immediately on nil connection

Matches Go commit: 723d319
```

---

## References

- **Go Commit**: `723d319` on `main` branch
- **Go Files**: 
  - `client/tunnel/tunnel.go` (Lines 281, 303-314)
  - `client/tunnel/tcpproxy.go` (Line 65)

---

**Migration Status**: ✅ Complete  
**Code Review Status**: ⏳ Pending  
**Testing Status**: ⏳ Pending (requires Cargo update)
