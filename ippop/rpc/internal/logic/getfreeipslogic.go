package logic

import (
	"context"

	"titan-ipoverlay/ippop/rpc/internal/svc"
	"titan-ipoverlay/ippop/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetFreeIPsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetFreeIPsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetFreeIPsLogic {
	return &GetFreeIPsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetFreeIPsLogic) GetFreeIPs(in *pb.GetFreeIPsReq) (*pb.GetFreeIPsResp, error) {
	ips := l.svcCtx.TunnelManager.GetFreeIPs(int(in.Count), in.FromHead)
	res := make([]*pb.FreeIPInfoPb, 0, len(ips))
	for _, ip := range ips {
		res = append(res, &pb.FreeIPInfoPb{
			Ip:      ip.IP,
			NodeIds: ip.NodeIDs,
		})
	}

	return &pb.GetFreeIPsResp{Ips: res}, nil
}
