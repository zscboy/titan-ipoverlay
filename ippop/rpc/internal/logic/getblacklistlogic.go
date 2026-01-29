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

func (l *GetBlacklistLogic) GetBlacklist(in *pb.GetBlacklistReq) (*pb.GetBlacklistResp, error) {
	count := int(in.Count)
	if count <= 0 {
		count = 100
	}
	ips, nextCursor, err := model.GetBlacklistPage(l.svcCtx.Redis, in.Cursor, count)
	if err != nil {
		return nil, err
	}

	return &pb.GetBlacklistResp{
		IpList:     ips,
		NextCursor: nextCursor,
	}, nil
}
