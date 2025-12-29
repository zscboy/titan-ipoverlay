mod bootstrap;
mod tunnel;
mod pb;

use clap::Parser;
use log::{info, error, LevelFilter};
use tokio::time::{sleep, Duration};
use crate::tunnel::{Tunnel, TunnelOptions};
use crate::bootstrap::BootstrapMgr;

#[derive(Parser, Debug)]
#[command(name = "titan-ipoverlay-client", version, about = "vms client")]
struct Args {
    #[arg(long, default_value = "./")]
    app_dir: String,

    #[arg(long, default_value = "")]
    direct_url: String,

    #[arg(long, required = true)]
    uuid: String,

    #[arg(long, default_value_t = 60)]
    udp_timeout: i32,

    #[arg(long, default_value_t = 3)]
    tcp_timeout: i32,

    #[arg(long, default_value_t = false)]
    debug: bool,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let args = Args::parse();

    let log_level = if args.debug {
        LevelFilter::Debug
    } else {
        LevelFilter::Info
    };

    env_logger::Builder::new()
        .filter_level(log_level)
        .init();

    let mut opts = TunnelOptions {
        uuid: args.uuid.clone(),
        udp_timeout: args.udp_timeout,
        tcp_timeout: args.tcp_timeout,
        bootstrap_mgr: None,
        direct_url: args.direct_url.clone(),
        version: env!("CARGO_PKG_VERSION").to_string(),
    };

    if args.direct_url.is_empty() {
        let bootstrap_mgr = BootstrapMgr::new(&args.app_dir).await?;
        if bootstrap_mgr.get_bootstraps().is_empty() {
            anyhow::bail!("no bootstrap nodes found");
        }
        opts.bootstrap_mgr = Some(bootstrap_mgr);
    }

    let mut tun = Tunnel::new(&opts)?;

    loop {
        match tun.connect().await {
            Ok(ws_stream) => {
                info!("tun connect success");
                if let Err(e) = tun.serve(*ws_stream).await {
                    error!("Tunnel serve error: {:?}", e);
                }
            }
            Err(e) => {
                error!("Connect failed: {:?}", e);
            }
        }

        if tun.is_destroy() {
            break;
        }

        info!("wait 10 seconds to retry connect");
        sleep(Duration::from_secs(10)).await;
    }

    Ok(())
}
