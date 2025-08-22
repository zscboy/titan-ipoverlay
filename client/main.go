package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
	"titan-ipoverlay/client/bootstrap"
	"titan-ipoverlay/client/tunnel"

	"github.com/urfave/cli/v2"
	"github.com/zeromicro/go-zero/core/logx"
)

const version = "1.0.0"

func main() {
	app := &cli.App{
		Name:    "titan-ipoverlay-client",
		Usage:   "vms client",
		Version: version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "app-dir",
				Usage: "--app-dir='./'",
				Value: "",
			},
			&cli.StringFlag{
				Name:  "direct-url",
				Usage: "--direct-url=http://localhost:41005/node/pop",
				Value: "",
			},
			&cli.StringFlag{
				Name:     "uuid",
				Usage:    "--uuid 08bd0658-1f61-11f0-8061-8bd115314f4c",
				Required: true,
				Value:    "",
			},

			&cli.IntFlag{
				Name:  "udp-timeout",
				Usage: "--udp-timeout 60, seconds",
				Value: 60,
			},
			&cli.IntFlag{
				Name:  "tcp-timeout",
				Usage: "--tcp-timeout 3, seconds",
				Value: 3,
			},
			&cli.BoolFlag{
				Name:  "debug",
				Usage: "--debug",
				Value: false,
			},
		},
		Before: func(cctx *cli.Context) error {
			return nil
		},
		Action: func(cctx *cli.Context) error {
			appDir := cctx.String("app-dir")
			directURL := cctx.String("direct-url")
			uuid := cctx.String("uuid")
			udpTimeout := cctx.Int("udp-timeout")
			tcpTimeout := cctx.Int("tcp-timeout")
			debug := cctx.Bool("debug")
			if debug {
				logx.SetLevel(logx.DebugLevel)
			} else {
				logx.SetLevel(logx.InfoLevel)
			}

			opts := tunnel.TunnelOptions{
				UUID:       uuid,
				UDPTimeout: udpTimeout,
				TCPTimeout: tcpTimeout,
				// BootstrapMgr: bootstrapMgr,
				DirectURL: directURL,
			}

			if len(directURL) == 0 {
				bootstrapMgr, err := bootstrap.NewBootstrapMgr(appDir)
				if err != nil {
					return err
				}

				if len(bootstrapMgr.Bootstraps()) == 0 {
					return fmt.Errorf("no bootstrap nodes found")
				}
				opts.BootstrapMgr = bootstrapMgr
			}

			tun, err := tunnel.NewTunnel(&opts)
			if err != nil {
				return err
			}

			if err = tun.Connect(); err != nil {
				return err
			}
			defer tun.Destroy()

			logx.Debugf("Start ip overlay success")
			ctx, cancel := context.WithCancel(cctx.Context)
			go tunServe(tun, cancel)

			sigChan := make(chan os.Signal, 2)
			signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
			for {
				select {
				case <-sigChan:
					return nil
				case <-ctx.Done():
					return nil
				}
			}
		},
	}

	if err := app.Run(os.Args); err != nil {
		logx.Error(err)
	}
}

func tunServe(tun *tunnel.Tunnel, cancel context.CancelFunc) {
	defer cancel()
	for {
		tun.Serve()

		if tun.IsDestroy() {
			return
		}

		var err error
		for {
			err = tun.Connect()
			if err == nil {
				break
			}

			logx.Error("wait 5 seconds to retry connect")
			time.Sleep(10 * time.Second)
		}
	}
}
