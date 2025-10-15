package logic

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"titan-ipoverlay/ippop/rpc/internal/svc"
	"titan-ipoverlay/ippop/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type KickNodeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewKickNodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *KickNodeLogic {
	return &KickNodeLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *KickNodeLogic) KickNode(in *pb.KickNodeReq) (*pb.UserOperationResp, error) {
	return l.kickNode(in.NodeId)
}

func (l *KickNodeLogic) kickNode(nodeId string) (*pb.UserOperationResp, error) {
	url := fmt.Sprintf("http://%s/node/kick?nodeid=%s", l.svcCtx.Config.APIServer, nodeId)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		buf, _ := io.ReadAll(resp.Body)
		return &pb.UserOperationResp{ErrMsg: fmt.Sprintf("status code %d, error:%s", resp.StatusCode, string(buf))}, nil
	}

	return &pb.UserOperationResp{Success: true}, nil
}
