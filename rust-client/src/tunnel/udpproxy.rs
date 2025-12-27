use std::sync::Arc;
use tokio::net::UdpSocket;
use log::{debug, error};
use anyhow::Result;
use std::time::Duration;
use crate::tunnel::TaskTunnel;

pub struct UdpProxy {
    pub id: String,
    socket: Arc<UdpSocket>,
    timeout: i32,
}

impl UdpProxy {
    pub fn new(id: String, socket: UdpSocket, timeout: i32) -> Self {
        Self {
            id,
            socket: Arc::new(socket),
            timeout,
        }
    }

    pub fn destroy(&self) {
        // UdpSocket doesn't have a close(), it drops when the last Arc is gone.
    }

    pub async fn write(&self, data: Vec<u8>) -> Result<()> {
        self.socket.send(&data).await?;
        Ok(())
    }

    pub async fn serve(&self, t: Arc<TaskTunnel>) -> Result<()> {
        let mut buf = [0u8; 32 * 1024];
        let id = self.id.clone();
        
        loop {
            let timeout = Duration::from_secs(self.timeout as u64);
            match tokio::time::timeout(timeout, self.socket.recv(&mut buf)).await {
                Ok(Ok(n)) => {
                    if let Err(e) = t.send_udp_data(&id, buf[..n].to_vec()).await {
                        error!("UDPProxy {} send_udp_data error: {}", id, e);
                        break;
                    }
                }
                Ok(Err(e)) => {
                    debug!("UDPProxy.serve recv error: {}, id: {}", e, id);
                    break;
                }
                Err(_) => {
                    debug!("UDPProxy {} timeout, closing", id);
                    break;
                }
            }
        }
        
        t.proxy_udps.lock().unwrap().remove(&id);
        Ok(())
    }
}
