# Titan IP Overlay - Rust Client

This is a 1:1 Rust port of the Titan IP Overlay edge client (originally written in Go).

## Features

- Full compatibility with Titan IP Overlay POP servers
- WebSocket-based tunnel communication with ProtoBuf encoding
- TCP and UDP proxy support
- Async I/O using Tokio
- 0-RTT session establishment
- Bootstrap server discovery
- Automatic reconnection

## Building

```bash
cargo build --release
```

## Running

```bash
./target/release/rust-client --uuid <your-node-uuid> [options]
```

### Options

- `--uuid <UUID>` - Node UUID (required)
- `--direct-url <URL>` - Direct POP server URL (optional, uses bootstrap discovery if not provided)
- `--app-dir <DIR>` - Application directory for bootstrap config (default: "./")
- `--tcp-timeout <SECONDS>` - TCP connection timeout in seconds (default: 3)
- `--udp-timeout <SECONDS>` - UDP session timeout in seconds (default: 60)
- `--debug` - Enable debug logging

### Example

```bash
./target/release/rust-client --uuid 08bd0658-1f61-11f0-8061-8bd115314f4c --direct-url http://localhost:41005/node/pop
```

## Architecture

The Rust client implements the same functionality as the Go client:

1. **Bootstrap Manager** (`bootstrap.rs`): Manages discovery and periodic updates of access points
2. **Tunnel** (`tunnel/mod.rs`): Core WebSocket tunnel management and message routing  
3. **TCP Proxy** (`tunnel/tcpproxy.rs`): Handles TCP session proxying with async buffering
4. **UDP Proxy** (`tunnel/udpproxy.rs`): Manages UDP sessions with timeouts

## Protocol

Communication uses ProtoBuf messages over WebSocket with the following message types:
- `PROXY_SESSION_CREATE` - Create new proxy session
- `PROXY_SESSION_DATA` - Transfer data  
- `PROXY_SESSION_CLOSE` - Close session
- `PROXY_UDP_DATA` - UDP datagram relay

## Dependencies

- Tokio - Async runtime
- tokio-tungstenite - WebSocket support
- prost - ProtoBuf encoding/decoding  
- reqwest - HTTP client
- clap - CLI argument parsing

## License

Same as the original Titan IP Overlay project.
