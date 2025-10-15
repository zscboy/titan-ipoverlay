package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

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
	url := fmt.Sprintf("http://%s/server/info", l.svcCtx.Config.APIServer)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		buf, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status code %d, error:%s", resp.StatusCode, string(buf))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	serverInfo := struct {
		Socks5Addr   string `json:"socks5_addr"`
		WSServerURL  string `json:"ws_server_url"`
		AccessSecret string `json:"access_secret"`
		AccessExpire int64  `json:"access_expire"`
	}{}

	err = json.Unmarshal(body, &serverInfo)
	if err != nil {
		return nil, err
	}

	return &pb.GetServerInfoResp{
		Socks5Addr:   serverInfo.Socks5Addr,
		WsServerUrl:  serverInfo.WSServerURL,
		AccessSecret: serverInfo.AccessSecret,
		AccessExpire: serverInfo.AccessExpire,
	}, nil
}
