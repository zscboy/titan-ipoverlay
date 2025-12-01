package ws

import (
	"fmt"
	"net/http"
	"time"

	"titan-ipoverlay/ippop/config"

	"github.com/golang-jwt/jwt/v4"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
)

type GetNodePopReq struct {
	NodeId string `form:"nodeid"`
}

type GetNodePopResp struct {
	ServerURL   string `json:"server_url"`
	AccessToken string `json:"access_token"`
}

type NodePop struct {
	cfg *config.Config
}

func NewNodePop(cfg *config.Config) *NodePop {
	return &NodePop{cfg: cfg}
}

func (pop *NodePop) ServeNodePop(w http.ResponseWriter, r *http.Request) {
	logx.Infof("NodeWS.ServeNodePop %s %s", r.URL.Path, r.URL.RawQuery)

	var req GetNodePopReq
	if err := httpx.Parse(r, &req); err != nil {
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}

	token, err := pop.generateJwtToken(pop.cfg.JwtAuth.AccessSecret, pop.cfg.JwtAuth.AccessExpire, req.NodeId)
	if err != nil {
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}

	domain := pop.cfg.Socks5.ServerIP
	if len(pop.cfg.WS.Domain) > 0 {
		domain = pop.cfg.WS.Domain
	}

	wsServerURl := fmt.Sprintf("ws://%s:%d/ws/node", domain, pop.cfg.WS.Port)
	resp := &GetNodePopResp{ServerURL: wsServerURl, AccessToken: string(token)}
	httpx.OkJsonCtx(r.Context(), w, resp)
}

func (pop *NodePop) generateJwtToken(secret string, expire int64, nodeId string) ([]byte, error) {
	claims := jwt.MapClaims{
		"user": nodeId,
		"exp":  time.Now().Add(time.Second * time.Duration(expire)).Unix(),
		"iat":  time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(secret))
	if err != nil {
		return nil, err
	}

	return []byte(tokenStr), nil
}
