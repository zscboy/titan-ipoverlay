package export

import (
	"context"
	"fmt"
	"net"
	"titan-ipoverlay/ippop/rpc/internal/config"
	"titan-ipoverlay/ippop/rpc/internal/server"
	"titan-ipoverlay/ippop/rpc/internal/svc"
	"titan-ipoverlay/ippop/rpc/pb"

	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

type RPCServerConfig config.Config

func NewRPCServer(c RPCServerConfig, nodeManager svc.NodeManager, userManger svc.UserManager, endpointProvider svc.EndpointProvider) *zrpc.RpcServer {
	ctx := svc.NewServiceContext(config.Config(c))
	ctx.NodeManager = nodeManager
	ctx.UserManager = userManger
	ctx.EndpointProvider = endpointProvider

	whitelist := make(map[string]bool)
	for _, ip := range c.Whitelist {
		whitelist[ip] = true
	}

	rpcServer := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		pb.RegisterServerAPIServer(grpcServer, server.NewServerAPIServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})

	rpcServer.AddUnaryInterceptors(whitelistInterceptor(whitelist))
	return rpcServer
}

func whitelistInterceptor(whitelist map[string]bool) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		peer, ok := peer.FromContext(ctx)
		if !ok {
			return nil, status.Error(codes.PermissionDenied, "peer info unavailable")
		}
		clientIP, _, err := net.SplitHostPort(peer.Addr.String())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid client address")
		}

		ip := net.ParseIP(clientIP)
		if ip == nil {
			return nil, status.Error(codes.InvalidArgument, "invalid ip format")
		}

		if ip.IsLoopback() || ip.IsPrivate() {
			return handler(ctx, req)
		}

		if !whitelist[clientIP] {
			return nil, status.Error(codes.PermissionDenied, fmt.Sprintf("IP %s not in whitelist", clientIP))
		}
		return handler(ctx, req)
	}
}
