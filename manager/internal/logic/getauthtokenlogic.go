package logic

import (
	"context"
	"fmt"
	"net"
	"time"

	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"

	"github.com/golang-jwt/jwt/v4"
	"github.com/zeromicro/go-zero/core/logx"
)

const userAdmin = "admin"

type GetAuthTokenLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetAuthTokenLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAuthTokenLogic {
	return &GetAuthTokenLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetAuthTokenLogic) GetAuthToken() (resp *types.GetAuthTokenResp, err error) {
	remoteIP := l.ctx.Value("Remote-IP")
	ip := net.ParseIP(remoteIP.(string))
	if ip == nil {
		return nil, fmt.Errorf("can not get ip")
	}

	if !ip.IsLoopback() && !l.isWhitelisted(remoteIP.(string)) {
		return nil, fmt.Errorf("ip %s not permission access", ip)
	}

	token, err := l.generateJwtToken(l.svcCtx.Config.JwtAuth.AccessSecret, l.svcCtx.Config.JwtAuth.AccessExpire, userAdmin)
	if err != nil {
		return nil, err
	}
	return &types.GetAuthTokenResp{Token: string(token)}, nil
}

func (l *GetAuthTokenLogic) isWhitelisted(remoteIP string) bool {
	for _, ip := range l.svcCtx.Config.Whitelist {
		if ip == remoteIP {
			return true
		}
	}
	return false
}

func (l *GetAuthTokenLogic) generateJwtToken(secret string, expire int64, nodeId string) ([]byte, error) {
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
