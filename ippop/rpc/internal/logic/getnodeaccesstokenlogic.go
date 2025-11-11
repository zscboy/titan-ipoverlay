package logic

import (
	"context"
	"time"

	"titan-ipoverlay/ippop/rpc/internal/svc"
	"titan-ipoverlay/ippop/rpc/pb"

	"github.com/golang-jwt/jwt/v4"
	"github.com/zeromicro/go-zero/core/logx"
)

type GetNodeAccessTokenLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetNodeAccessTokenLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetNodeAccessTokenLogic {
	return &GetNodeAccessTokenLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetNodeAccessTokenLogic) GetNodeAccessToken(in *pb.GetNodeAccessTokenReq) (*pb.GetNodeAccessTokenResp, error) {
	secret, expire, err := l.svcCtx.GetAuth()
	if err != nil {
		return nil, err
	}

	return l.generateJwtToken(secret, expire, in.NodeId)
}

func (l *GetNodeAccessTokenLogic) generateJwtToken(secret string, expire int64, nodeId string) (*pb.GetNodeAccessTokenResp, error) {
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

	return &pb.GetNodeAccessTokenResp{Token: tokenStr}, nil
}
