package logic

import (
	"context"

	"titan-ipoverlay/ippop/rpc/internal/svc"
	"titan-ipoverlay/ippop/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetServerInfoLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetServerInfoLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetServerInfoLogic {
	return &GetServerInfoLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetServerInfoLogic) GetServerInfo(in *pb.Empty) (*pb.GetServerInfoResp, error) {
	return l.getServerInfo()
}

func (l *GetServerInfoLogic) getServerInfo() (*pb.GetServerInfoResp, error) {
	secret, expire, err := l.svcCtx.GetAuth()
	if err != nil {
		return nil, err
	}
	socks5Addr, err := l.svcCtx.GetSocks5Addr()
	if err != nil {
		return nil, err
	}
	wsURL, err := l.svcCtx.GetWSURL()
	if err != nil {
		return nil, err
	}

	return &pb.GetServerInfoResp{
		Socks5Addr:   socks5Addr,
		WsServerUrl:  wsURL,
		AccessSecret: secret,
		AccessExpire: expire,
	}, nil
}
