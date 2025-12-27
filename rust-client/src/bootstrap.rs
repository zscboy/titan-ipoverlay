use serde::{Deserialize, Serialize};
use std::fs;
use std::path::PathBuf;
use std::sync::{Arc, Mutex};
use std::time::Duration;
use log::{error, info};
use anyhow::Result;

const UPDATE_INTERVALS: u64 = 12 * 60 * 60;
const BOOTSTRAP_FILE: &str = "bootstrap.json";
const DEFAULT_BOOTSTRAP: &[&str] = &[
    "https://pcdn.titannet.io/test4/ipservice/bootstrap.json",
    "http://8.209.251.146:8080/bootstrap.json",
    "http://47.85.83.7:8080/bootstrap.json",
]; // Fallback bootstrap URLs

#[derive(Serialize, Deserialize, Debug, Clone)]
pub struct Config {
    pub bootstraps: Vec<String>,
}

pub struct BootstrapMgr {
    dir: PathBuf,
    bootstraps: Arc<Mutex<Vec<String>>>,
}

impl BootstrapMgr {
    pub async fn new(dir: &str) -> Result<Arc<Self>> {
        let dir_path = PathBuf::from(dir);
        if !dir_path.exists() {
            fs::create_dir_all(&dir_path)?;
        }

        let bootstrap_file_path = dir_path.join(BOOTSTRAP_FILE);
        let content = if bootstrap_file_path.exists() {
            fs::read_to_string(&bootstrap_file_path)?
        } else {
            let default_content = serde_json::to_string(&Config { bootstraps: DEFAULT_BOOTSTRAP.iter().map(|s| s.to_string()).collect() })?;
            fs::write(&bootstrap_file_path, &default_content)?;
            default_content
        };

        let cfg: Config = serde_json::from_str(&content)?;
        let mgr = Arc::new(Self {
            dir: dir_path,
            bootstraps: Arc::new(Mutex::new(cfg.bootstraps)),
        });

        let mgr_clone = mgr.clone();
        mgr_clone.get_bootstraps_from_server().await?;

        let mgr_periodic = mgr.clone();
        tokio::spawn(async move {
            mgr_periodic.timed_update().await;
        });

        Ok(mgr)
    }

    pub fn get_bootstraps(&self) -> Vec<String> {
        self.bootstraps.lock().unwrap().clone()
    }

    async fn timed_update(&self) {
        let mut interval = tokio::time::interval(Duration::from_secs(UPDATE_INTERVALS));
        loop {
            interval.tick().await;
            if let Err(e) = self.get_bootstraps_from_server().await {
                error!("BootstrapMgr.timed_update failed: {:?}", e);
            }
        }
    }

    async fn get_bootstraps_from_server(&self) -> Result<()> {
        let current_bootstraps = self.get_bootstraps();
        for url in current_bootstraps {
            match self.http_get(&url).await {
                Ok(bytes) => {
                    let bootstrap_file_path = self.dir.join(BOOTSTRAP_FILE);
                    fs::write(&bootstrap_file_path, &bytes)?;

                    let cfg: Config = serde_json::from_slice(&bytes)?;
                    let mut bootstraps = self.bootstraps.lock().unwrap();
                    *bootstraps = cfg.bootstraps;
                    info!("Bootstraps updated from server: {}", url);
                    return Ok(());
                }
                Err(e) => {
                    error!("BootstrapMgr.get_bootstraps_from_server failed for {}: {:?}", url, e);
                }
            }
        }
        Ok(())
    }

    async fn http_get(&self, url: &str) -> Result<Vec<u8>> {
        let client = reqwest::Client::builder()
            .timeout(Duration::from_secs(3))
            .build()?;
        let resp = client.get(url).send().await?;
        if resp.status().is_success() {
            Ok(resp.bytes().await?.to_vec())
        } else {
            let status = resp.status();
            let body = resp.text().await?;
            anyhow::bail!("StatusCode {}, msg: {}", status, body)
        }
    }
}
