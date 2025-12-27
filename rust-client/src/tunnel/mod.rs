use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::Duration;
use tokio::net::TcpStream;
use tokio::sync::{mpsc, oneshot};
use tokio_tungstenite::{connect_async, tungstenite::protocol::Message as WsMessage, MaybeTlsStream, WebSocketStream};
use futures_util::{StreamExt, SinkExt};
use log::{info, error, debug};
use anyhow::{Result, Context};
use serde::{Deserialize, Serialize};
use prost::Message as ProstMessage;
use crate::pb::pb;
use crate::bootstrap::BootstrapMgr;
use crate::tunnel::tcpproxy::TcpProxy;
use crate::tunnel::udpproxy::UdpProxy;


pub mod tcpproxy;
pub mod udpproxy;

pub struct TunnelOptions {
    pub uuid: String,
    pub udp_timeout: i32,
    pub tcp_timeout: i32,
    pub bootstrap_mgr: Option<Arc<BootstrapMgr>>,
    pub direct_url: String,
    pub version: String,
}

#[derive(Serialize, Deserialize, Debug)]
pub struct Pop {
    pub server_url: String,
    pub access_token: String,
}

pub struct Tunnel {
    uuid: String,
    write_tx: Arc<Mutex<mpsc::Sender<WsMessage>>>,
    proxy_sessions: Arc<Mutex<HashMap<String, Arc<TcpProxy>>>>,
    proxy_udps: Arc<Mutex<HashMap<String, Arc<UdpProxy>>>>,
    is_destroy: Arc<Mutex<bool>>,
    udp_timeout: i32,
    tcp_timeout: i32,
    bootstrap_mgr: Option<Arc<BootstrapMgr>>,
    direct_url: String,
    version: String,
    close_tx: Option<oneshot::Sender<()>>,
}

impl Tunnel {
    pub fn new(opts: &TunnelOptions) -> Result<Self> {
        let (write_tx, _) = mpsc::channel(1); 
        Ok(Self {
            uuid: opts.uuid.clone(),
            write_tx: Arc::new(Mutex::new(write_tx)),
            proxy_sessions: Arc::new(Mutex::new(HashMap::new())),
            proxy_udps: Arc::new(Mutex::new(HashMap::new())),
            is_destroy: Arc::new(Mutex::new(false)),
            udp_timeout: opts.udp_timeout,
            tcp_timeout: opts.tcp_timeout,
            bootstrap_mgr: opts.bootstrap_mgr.clone(),
            direct_url: opts.direct_url.clone(),
            version: opts.version.clone(),
            close_tx: None,
        })
    }

    pub async fn connect(&mut self) -> Result<Box<WebSocketStream<MaybeTlsStream<TcpStream>>>> {
        let pop = self.get_pop().await?;
        let url_str = format!("{}?id={}&os=linux&version={}", pop.server_url, self.uuid, self.version);
        info!("Tunnel.Connect, new tun {} {}", pop.server_url, pop.access_token);
        
        let request = http::Request::builder()
            .uri(&url_str)
            .header("Authorization", format!("Bearer {}", pop.access_token))
            .header("Sec-WebSocket-Key", tokio_tungstenite::tungstenite::handshake::client::generate_key())
            .header("Host", reqwest::Url::parse(&url_str)?.host_str().unwrap_or_default())
            .header("Upgrade", "websocket")
            .header("Connection", "Upgrade")
            .header("Sec-WebSocket-Version", "13")
            .body(())?;

        let (ws_stream, _) = connect_async(request).await?;
        info!("Tunnel.Connect, new tun {}", pop.server_url);

        Ok(Box::new(ws_stream))
    }

    async fn get_pop(&self) -> Result<Pop> {
        let access_points = if !self.direct_url.is_empty() {
            vec![self.direct_url.clone()]
        } else {
            self.get_access_points().await?
        };

        if access_points.is_empty() {
            anyhow::bail!("no access point found");
        }

        let client = reqwest::Client::new();
        for ap in access_points {
            let url = format!("{}?nodeid={}", ap, self.uuid);
            match client.get(&url).send().await {
                Ok(resp) if resp.status().is_success() => {
                    let pop: Pop = resp.json().await?;
                    return Ok(pop);
                }
                Ok(resp) => error!("Tunnel.get_pop status: {}", resp.status()),
                Err(e) => error!("Tunnel.get_pop error: {}", e),
            }
        }

        anyhow::bail!("no pop found")
    }

    async fn get_access_points(&self) -> Result<Vec<String>> {
        let mgr = self.bootstrap_mgr.as_ref().context("no bootstrap mgr")?;
        let client = reqwest::Client::new();

        #[derive(Deserialize)]
        struct APConfig {
            accesspoints: Vec<String>,
        }

        for url in mgr.get_bootstraps() {
            match client.get(&url).send().await {
                Ok(resp) if resp.status().is_success() => {
                    let cfg: APConfig = resp.json().await?;
                    return Ok(cfg.accesspoints);
                }
                _ => continue,
            }
        }
        Ok(vec![])
    }

    pub fn destroy(&self) {
        let mut destroy = self.is_destroy.lock().unwrap();
        *destroy = true;
    }

    pub fn is_destroy(&self) -> bool {
        *self.is_destroy.lock().unwrap()
    }

    pub async fn serve(&self, ws_stream: WebSocketStream<MaybeTlsStream<TcpStream>>) -> Result<()> {
        let (mut write_half, mut read_half) = ws_stream.split();
        let (write_tx, mut write_rx) = mpsc::channel::<WsMessage>(1024);

        // Update write_tx in Tunnel (requires interior mutability or just passing around)
        // For simplicity in this port, we'll spawn a write loop task
        let _write_task = tokio::spawn(async move {
            while let Some(msg) = write_rx.recv().await {
                if let Err(e) = write_half.send(msg).await {
                    error!("Tunnel write error: {}", e);
                    break;
                }
            }
        });

        // Update the shared write_tx for existing/new sessions
        {
            let mut w_guard = self.write_tx.lock().unwrap();
            *w_guard = write_tx.clone();
        }

        while let Some(msg_result) = read_half.next().await {
            match msg_result {
                Ok(WsMessage::Binary(p)) => {
                    if let Err(e) = self.on_tunnel_msg(p).await {
                        error!("on_tunnel_msg error: {}", e);
                    }
                }
                Ok(WsMessage::Ping(p)) => {
                    let _ = write_tx.send(WsMessage::Pong(p)).await;
                }
                Ok(WsMessage::Close(_)) => break,
                Err(e) => {
                    error!("WebSocket read error: {}", e);
                    break;
                }
                _ => {}
            }
        }

        self.on_close();
        Ok(())
    }

    async fn on_tunnel_msg(&self, p: Vec<u8>) -> Result<()> {
        let msg = pb::Message::decode(&p[..])?;
        debug!("Tunnel.on_tunnel_msg type: {:?}, sid: {}", msg.r#type(), msg.session_id);

        match msg.r#type() {
            pb::MessageType::ProxySessionCreate => {
                let self_clone = Arc::new(self.clone_for_task());
                if let Err(e) = self_clone.create_proxy_session(msg.session_id.clone(), msg.payload.clone()).await {
                    error!("create_proxy_session error: {}", e);
                }
            }
            pb::MessageType::ProxySessionData => {
                let sessions = self.proxy_sessions.lock().unwrap();
                if let Some(proxy) = sessions.get(&msg.session_id) {
                    proxy.write(&msg.payload).await?;
                }
            }
            pb::MessageType::ProxySessionClose => {
                let mut sessions = self.proxy_sessions.lock().unwrap();
                if let Some(proxy) = sessions.remove(&msg.session_id) {
                    proxy.close_by_server();
                }
            }
            pb::MessageType::ProxyUdpData => {
                let sid = msg.session_id.clone();
                let payload = msg.payload.clone();
                let self_clone = Arc::new(self.clone_for_task());
                tokio::spawn(async move {
                    if let Err(e) = self_clone.on_proxy_udp_data_from_tunnel(sid, payload).await {
                        error!("on_proxy_udp_data_from_tunnel error: {}", e);
                    }
                });
            }
            _ => error!("Unsupported message type: {:?}", msg.r#type()),
        }
        Ok(())
    }

    // Helper to clone session identifiers and timeouts
    fn clone_for_task(&self) -> TaskTunnel {
        TaskTunnel {
            write_tx: self.write_tx.clone(),
            proxy_sessions: self.proxy_sessions.clone(),
            proxy_udps: self.proxy_udps.clone(),
            tcp_timeout: self.tcp_timeout,
            udp_timeout: self.udp_timeout,
        }
    }

    fn on_close(&self) {
        self.clear_proxys();
    }

    fn clear_proxys(&self) {
        let mut sessions = self.proxy_sessions.lock().unwrap();
        for (_, proxy) in sessions.drain() {
            proxy.destroy();
        }
        let mut udps = self.proxy_udps.lock().unwrap();
        for (_, udp) in udps.drain() {
            udp.destroy();
        }
    }
}

pub struct TaskTunnel {
    write_tx: Arc<Mutex<mpsc::Sender<WsMessage>>>,
    proxy_sessions: Arc<Mutex<HashMap<String, Arc<TcpProxy>>>>,
    proxy_udps: Arc<Mutex<HashMap<String, Arc<UdpProxy>>>>,
    tcp_timeout: i32,
    udp_timeout: i32,
}

impl TaskTunnel {
    async fn create_proxy_session(&self, session_id: String, payload: Vec<u8>) -> Result<()> {
        let dest_addr = pb::DestAddr::decode(&payload[..])?;
        debug!("Tunnel.create_proxy_session dest: {}", dest_addr.addr);

        let proxy = Arc::new(TcpProxy::new(session_id.clone()));
        self.proxy_sessions.lock().unwrap().insert(session_id.clone(), proxy.clone());

        let _write_tx_reply = self.write_tx.clone();
        let sid_reply = session_id.clone();
        
        let self_clone = Arc::new(self.clone());
        tokio::spawn(async move {
             match tokio::time::timeout(
                Duration::from_secs(self_clone.tcp_timeout as u64),
                TcpStream::connect(&dest_addr.addr)
            ).await {
                Ok(Ok(stream)) => {
                    let _ = self_clone.create_proxy_session_reply(&sid_reply, None).await;
                    proxy.proxy_conn(stream, self_clone).await;
                }
                Ok(Err(e)) => {
                    // Categorize dial error for diagnostics
                    let error_tag = if e.kind() == std::io::ErrorKind::TimedOut {
                        "[NODE_TIMEOUT]"
                    } else {
                        "[NODE_UNSTABLE]"
                    };
                    error!("{} dial {} failed: {}", error_tag, dest_addr.addr, e);
                    let _ = self_clone.create_proxy_session_reply(&sid_reply, Some(e.to_string())).await;
                    let _ = self_clone.on_proxy_conn_close(&sid_reply).await;
                }
                Err(_) => {
                    error!("[NODE_TIMEOUT] dial {} failed: timeout", dest_addr.addr);
                    let _ = self_clone.create_proxy_session_reply(&sid_reply, Some("timeout".to_string())).await;
                    let _ = self_clone.on_proxy_conn_close(&sid_reply).await;
                }
            }
        });

        Ok(())
    }

    async fn create_proxy_session_reply(&self, session_id: &str, err_msg: Option<String>) -> Result<()> {
        let reply = pb::CreateSessionReply {
            success: err_msg.is_none(),
            err_msg: err_msg.unwrap_or_default(),
        };
        let mut payload = Vec::new();
        reply.encode(&mut payload)?;

        let msg = pb::Message {
            r#type: pb::MessageType::ProxySessionCreate as i32,
            session_id: session_id.to_string(),
            payload,
        };
        self.write_pb_message(msg).await
    }

    pub async fn on_proxy_conn_close(&self, session_id: &str) -> Result<()> {
        let msg = pb::Message {
            r#type: pb::MessageType::ProxySessionClose as i32,
            session_id: session_id.to_string(),
            payload: Vec::new(),
        };
        self.write_pb_message(msg).await
    }

    pub async fn send_session_data(&self, session_id: &str, data: Vec<u8>) -> Result<()> {
        let msg = pb::Message {
            r#type: pb::MessageType::ProxySessionData as i32,
            session_id: session_id.to_string(),
            payload: data,
        };
        self.write_pb_message(msg).await
    }

    async fn write_pb_message(&self, msg: pb::Message) -> Result<()> {
        let mut buf = Vec::new();
        msg.encode(&mut buf)?;
        let tx = {
            let guard = self.write_tx.lock().unwrap();
            guard.clone()
        };
        tx.send(WsMessage::Binary(buf)).await.map_err(|_| anyhow::anyhow!("channel closed"))
    }

    async fn on_proxy_udp_data_from_tunnel(&self, sid: String, payload: Vec<u8>) -> Result<()> {
        let udp_data = pb::UdpData::decode(&payload[..])?;

        // Check if proxy already exists
        let existing_proxy = {
            let udps = self.proxy_udps.lock().unwrap();
            udps.get(&sid).cloned()
        };
        
        if let Some(proxy) = existing_proxy {
            proxy.write(udp_data.data).await?;
            return Ok(());
        }

        // Create new UDP proxy
        let raddr: std::net::SocketAddr = udp_data.addr.parse()?;
        let socket = tokio::net::UdpSocket::bind("0.0.0.0:0").await?;
        socket.connect(raddr).await?;

        let proxy = Arc::new(UdpProxy::new(sid.clone(), socket, self.udp_timeout));
        proxy.write(udp_data.data).await?;
        
        {
            let mut udps = self.proxy_udps.lock().unwrap();
            udps.insert(sid, proxy.clone());
        }

        let self_clone = Arc::new(self.clone());
        tokio::spawn(async move {
            if let Err(e) = proxy.serve(self_clone).await {
                error!("UDPProxy.serve error: {}", e);
            }
        });

        Ok(())
    }

    pub async fn send_udp_data(&self, session_id: &str, data: Vec<u8>) -> Result<()> {
        let msg = pb::Message {
            r#type: pb::MessageType::ProxyUdpData as i32,
            session_id: session_id.to_string(),
            payload: data,
        };
        self.write_pb_message(msg).await
    }
}

impl Clone for TaskTunnel {
    fn clone(&self) -> Self {
        Self {
            write_tx: self.write_tx.clone(),
            proxy_sessions: self.proxy_sessions.clone(),
            proxy_udps: self.proxy_udps.clone(),
            tcp_timeout: self.tcp_timeout,
            udp_timeout: self.udp_timeout,
        }
    }
}
