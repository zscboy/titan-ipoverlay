package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"titan-ipoverlay/manager/internal/config"
	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"
	"titan-ipoverlay/manager/model"

	"github.com/golang-jwt/jwt/v4"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	NodeAccessPointDefaultKey = "Default"
)

type Location struct {
	Country  string `json:"country"`
	Province string `json:"province"`
	City     string `json:"city"`
	IP       string `json:"ip"`
}

type LocationData struct {
	Location *Location `json:"location"`
}
type LocationResp struct {
	Code int           `json:"code"`
	Data *LocationData `json:"data"`
	Msg  string        `json:"msg"`
}

type GetNodePopLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetNodePopLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetNodePopLogic {
	return &GetNodePopLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetNodePopLogic) GetNodePop(req *types.GetNodePopReq) (resp *types.GetNodePopResp, err error) {
	podConfig, err := l.allocatePop(req)
	if err != nil {
		return nil, err
	}

	server := l.getPodServer(podConfig.Id)
	if server == nil {
		return nil, fmt.Errorf("not found pop for node %s", req.NodeId)
	}

	tokenBytes, err := l.generateJwtToken(server.AccessSecret, server.AccessExpire, req.NodeId)
	if err != nil {
		return nil, err
	}

	logx.Debugf("GetNodePop, %s accessPoint %s, getTokenResp:%s", req.NodeId, podConfig.WSURL, string(tokenBytes))
	return &types.GetNodePopResp{ServerURL: podConfig.WSURL, AccessToken: string(tokenBytes)}, nil
}

func (l *GetNodePopLogic) allocatePop(req *types.GetNodePopReq) (*config.Pop, error) {
	ipValue := l.ctx.Value("Remote-IP")
	ip := ipValue.(string)
	if len(ip) == 0 {
		return nil, fmt.Errorf("can not get remote ip")
	}

	popID, nodeIP, err := model.GetNodePopIP(l.svcCtx.Redis, req.NodeId)
	if err != nil {
		return nil, err
	}

	if len(popID) > 0 {
		for _, pop := range l.svcCtx.Config.Pops {
			if pop.Id == string(popID) {
				if ip == string(nodeIP) {
					logx.Debugf("node %s ip %s already exist pop %s", req.NodeId, nodeIP, pop.Id)
					return &pop, nil
				}

				logx.Debugf("node %s change ip %s to %s, old pop:%s, will check location info", req.NodeId, string(nodeIP), ip, popID)
				break
			}
		}
	}

	location, err := l.getLocalInfo(ip)
	if err != nil {
		logx.Errorf("getLocalInfo %v", err)
		return nil, fmt.Errorf("getLocalInfo failed:%v", err)
	}

	popIDs := make([]string, 0, len(l.svcCtx.Config.Pops))
	for _, pop := range l.svcCtx.Config.Pops {
		popIDs = append(popIDs, pop.Id)

	}

	nodeCountMap, err := model.NodeCountOfPops(l.ctx, l.svcCtx.Redis, popIDs)
	if err != nil {
		return nil, err
	}

	for _, pop := range l.svcCtx.Config.Pops {
		if pop.Area == location.Country {
			count, ok := nodeCountMap[pop.Id]
			if !ok {
				continue
			}

			if count >= int64(pop.MaxCount) {
				continue
			}

			if err := model.SetNodePopIP(l.svcCtx.Redis, req.NodeId, pop.Id, ip); err != nil {
				logx.Errorf("allocatePop SetNodePop error %v", err)
			}

			logx.Debugf("new node %s location %v, allocate pop:%s", req.NodeId, location, pop.Id)
			return &pop, nil
		}
	}

	for _, pop := range l.svcCtx.Config.Pops {
		if pop.Area == l.svcCtx.Config.DefaultArea {
			if err := model.SetNodePopIP(l.svcCtx.Redis, req.NodeId, pop.Id, ip); err != nil {
				logx.Errorf("allocatePop SetNodePop error %v", err)
			}

			logx.Debugf("new node %s location %v not exit, allocate default area %v, allocate pop:%s", req.NodeId, location, l.svcCtx.Config.DefaultArea, pop.Id)
			return &pop, nil
		}
	}

	return nil, fmt.Errorf("no pop found for %s, location:%v", req.NodeId, location)
}

func (l *GetNodePopLogic) getPodServer(id string) *svc.Pop {
	for podID, pop := range l.svcCtx.Pops {
		if podID == id {
			return pop
		}
	}
	return nil
}

func (l *GetNodePopLogic) getLocalInfo(ip string) (*Location, error) {
	loc, err := model.GetIPLocation(l.svcCtx.Redis, ip)
	if err == nil {
		return &Location{IP: loc.IP, City: loc.City, Province: loc.Province, Country: loc.Country}, nil
	}

	client := &http.Client{
		Timeout: 3 * time.Second,
	}

	url := fmt.Sprintf("%s?key=%s&ip=%s&language=en", l.svcCtx.Config.Geo.API, l.svcCtx.Config.Geo.Key, ip)
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bs, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("StatusCode %d, msg:%s", resp.StatusCode, string(bs))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	locationResp := &LocationResp{}
	err = json.Unmarshal(body, locationResp)
	if err != nil {
		return nil, err
	}

	if locationResp.Code != 0 && locationResp.Code != 200 {
		return nil, fmt.Errorf("code:%d, msg:%s", locationResp.Code, locationResp.Msg)
	}

	location := locationResp.Data.Location
	redisIPLocation := model.IPLocation{IP: location.IP, City: location.City, Province: location.Province, Country: location.Country}

	if err := model.SaveIPLocation(l.svcCtx.Redis, &redisIPLocation); err != nil {
		logx.Errorf("SaveIPLocation:%v", err)
	}

	return location, nil
}

func (l *GetNodePopLogic) generateJwtToken(secret string, expire int64, nodeId string) ([]byte, error) {
	claims := jwt.MapClaims{
		"user": nodeId,
		"exp":  time.Now().Add(time.Second * time.Duration(expire)).Unix(),
		"iat":  time.Now().Add(-5 * time.Minute).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(secret))
	if err != nil {
		return nil, err
	}

	return []byte(tokenStr), nil
}
