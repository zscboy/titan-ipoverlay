use std::sync::Arc;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;
use tokio::sync::{mpsc, Mutex as TokioMutex, Notify};
use log::{debug, error};
use anyhow::Result;
use crate::tunnel::TaskTunnel;

pub struct TcpProxy {
    pub id: String,
    write_tx: mpsc::Sender<Vec<u8>>,
    write_rx: TokioMutex<Option<mpsc::Receiver<Vec<u8>>>>,
    is_close_by_server: Arc<std::sync::Mutex<bool>>,
    shutdown_notify: Arc<Notify>,
}

impl TcpProxy {
    pub fn new(id: String) -> Self {
        let (write_tx, write_rx) = mpsc::channel::<Vec<u8>>(1024);
        
        Self {
            id,
            write_tx,
            write_rx: TokioMutex::new(Some(write_rx)),
            is_close_by_server: Arc::new(std::sync::Mutex::new(false)),
            shutdown_notify: Arc::new(Notify::new()),
        }
    }

    pub async fn write(&self, data: &[u8]) -> Result<()> {
        // If notify was triggered, don't allow more writes
        self.write_tx.send(data.to_vec()).await.map_err(|_| anyhow::anyhow!("channel closed"))
    }

    pub fn close_by_server(&self) {
        {
            let mut guard = self.is_close_by_server.lock().unwrap();
            *guard = true;
        }
        self.shutdown_notify.notify_waiters();
    }

    pub fn destroy(&self) {
        self.shutdown_notify.notify_waiters();
    }

    pub async fn proxy_conn(&self, stream: TcpStream, t: Arc<TaskTunnel>) {
        let id = self.id.clone();
        let (mut reader, mut writer) = stream.into_split();
        
        // Take the receiver
        let mut rx_guard = self.write_rx.lock().await;
        let mut rx = rx_guard.take().expect("TcpProxy started twice or receiver gone");
        drop(rx_guard);

        // Notify for shutdown
        let shutdown_write = self.shutdown_notify.clone();
        let shutdown_read = self.shutdown_notify.clone();

        // Spawn WRITER task (Tunnel -> Remote Server)
        let id_for_writer = id.clone();
        tokio::spawn(async move {
            loop {
                tokio::select! {
                    msg = rx.recv() => {
                        if let Some(data) = msg {
                            if let Err(e) = writer.write_all(&data).await {
                                error!("TCPProxy {} write error: {}", id_for_writer, e);
                                break;
                            }
                        } else {
                            break;
                        }
                    }
                    _ = shutdown_write.notified() => {
                        debug!("TCPProxy {} writer task shutting down", id_for_writer);
                        break;
                    }
                }
            }
            // Ensure writer is closed
            let _ = writer.shutdown().await;
        });

        // Run READER loop in current task (Remote Server -> Tunnel)
        let mut buf = [0u8; 32 * 1024];
        loop {
            tokio::select! {
                res = reader.read(&mut buf) => {
                    match res {
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
                _ = shutdown_read.notified() => {
                    debug!("TCPProxy {} reader loop shutting down", id);
                    break;
                }
            }
        }
        
        // Final cleanup
        if !*self.is_close_by_server.lock().unwrap() {
            let _ = t.on_proxy_conn_close(&id).await;
        }
        
        t.proxy_sessions.lock().unwrap().remove(&id);
    }
}
