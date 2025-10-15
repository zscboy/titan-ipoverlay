package logic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"titan-ipoverlay/ippop/rpc/internal/svc"
	"titan-ipoverlay/ippop/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type RemoveBlacklistLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRemoveBlacklistLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RemoveBlacklistLogic {
	return &RemoveBlacklistLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *RemoveBlacklistLogic) RemoveBlacklist(in *pb.RemoveBlacklistReq) (*pb.UserOperationResp, error) {
	// todo: add your logic here and delete this line

	return l.removeBlacklist(in.NodeId)
}

func (l *RemoveBlacklistLogic) removeBlacklist(nodeId string) (*pb.UserOperationResp, error) {
	url := fmt.Sprintf("http://%s/node/blacklist/remove", l.svcCtx.Config.APIServer)

	removeBlacklistReq := struct {
		NodeID string `json:"node_id"`
	}{
		NodeID: nodeId,
	}

	jsonData, err := json.Marshal(removeBlacklistReq)
	if err != nil {
		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
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
