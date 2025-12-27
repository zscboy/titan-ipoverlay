use std::sync::Arc;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;
use tokio::sync::{mpsc, Mutex as TokioMutex};
use log::{debug, error};
use anyhow::Result;
use crate::tunnel::TaskTunnel;

pub struct TcpProxy {
    pub id: String,
    conn: Arc<TokioMutex<Option<TcpStream>>>,
    is_close_by_server: Arc<std::sync::Mutex<bool>>,
    write_tx: mpsc::Sender<Vec<u8>>,
}

impl TcpProxy {
    pub fn new(id: String) -> Self {
        let (write_tx, mut write_rx) = mpsc::channel::<Vec<u8>>(1024);
        let id_clone = id.clone();
        
        let conn_holder = Arc::new(TokioMutex::new(None::<TcpStream>));
        let conn_holder_clone = conn_holder.clone();

        tokio::spawn(async move {
            while let Some(data) = write_rx.recv().await {
                let mut guard = conn_holder_clone.lock().await;
                if let Some(ref mut stream) = *guard {
                    if let Err(e) = stream.write_all(&data).await {
                        error!("TCPProxy {} write error: {}", id_clone, e);
                        break;
                    }
                } else {
                    // Wait for connection
                    drop(guard);
                    tokio::time::sleep(tokio::time::Duration::from_millis(10)).await;
                }
            }
        });

        Self {
            id,
            conn: conn_holder,
            is_close_by_server: Arc::new(std::sync::Mutex::new(false)),
            write_tx,
        }
    }

    pub async fn set_conn(&self, stream: TcpStream) {
        let mut guard = self.conn.lock().await;
        *guard = Some(stream);
    }

    pub async fn write(&self, data: &[u8]) -> Result<()> {
        self.write_tx.send(data.to_vec()).await.map_err(|_| anyhow::anyhow!("channel closed"))
    }

    pub fn close_by_server(&self) {
        let mut guard = self.is_close_by_server.lock().unwrap();
        *guard = true;
        // Connection will be dropped when TokioMutex is dropped
    }

    pub fn destroy(&self) {
        // Connection will be dropped when Arc is dropped
    }

    pub async fn proxy_conn(&self, t: Arc<TaskTunnel>) {
        let id = self.id.clone();
        
        let stream = {
            let mut guard = self.conn.lock().await;
            guard.take()
        };

        if let Some(mut s) = stream {
            let mut buf = [0u8; 32 * 1024];
            loop {
                match s.read(&mut buf).await {
                    Ok(0) => break,
                    Ok(n) => {
                        if let Err(e) = t.send_session_data(&id, buf[..n].to_vec()).await {
                            error!("TCPProxy {} send_session_data error: {}", id, e);
                            break;
                        }
                    }
                    Err(e) => {
                        debug!("TCPProxy.proxy_conn read error: {}, id: {}", e, id);
                        break;
                    }
                }
            }
            
            if !*self.is_close_by_server.lock().unwrap() {
                let _ = t.on_proxy_conn_close(&id).await;
            }
            
            t.proxy_sessions.lock().unwrap().remove(&id);
        }
    }
}
