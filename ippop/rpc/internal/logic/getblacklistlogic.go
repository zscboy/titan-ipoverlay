package logic

import (
	"context"

	"titan-ipoverlay/ippop/model"
	"titan-ipoverlay/ippop/rpc/internal/svc"
	"titan-ipoverlay/ippop/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetBlacklistLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetBlacklistLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetBlacklistLogic {
	return &GetBlacklistLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetBlacklistLogic) GetBlacklist(in *pb.Empty) (*pb.GetBlacklistResp, error) {
	ips, err := model.GetBlacklist(l.svcCtx.Redis)
	if err != nil {
		return nil, err
	}

	return &pb.GetBlacklistResp{IpList: ips}, nil
}
