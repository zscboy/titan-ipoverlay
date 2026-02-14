package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"
	"titan-ipoverlay/manager/model"

	"github.com/golang-jwt/jwt/v4"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
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

//	type LocationData struct {
//		Location *Location `json:"location"`
//	}
type LocationResp struct {
	Code int       `json:"code"`
	Data *Location `json:"data"`
	Msg  string    `json:"msg"`
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
	popEntity, err := l.allocatePop(req)
	if err != nil {
		return nil, err
	}

	tokenBytes, err := l.generateJwtToken(popEntity.AccessSecret, popEntity.AccessExpire, req.NodeId)
	if err != nil {
		return nil, err
	}

	logx.Debugf("GetNodePop, %s accessPoint %s, getTokenResp:%s", req.NodeId, popEntity.Config.WSURL, string(tokenBytes))
	return &types.GetNodePopResp{ServerURL: popEntity.Config.WSURL, AccessToken: string(tokenBytes)}, nil
}

func (l *GetNodePopLogic) allocatePop(req *types.GetNodePopReq) (*svc.Pop, error) {
	ipValue := l.ctx.Value("Remote-IP")
	ip := ipValue.(string)
	if len(ip) == 0 {
		return nil, fmt.Errorf("can not get remote ip")
	}

	// 1. Check Blacklist Priority (P1)
	_, isBlacklisted := l.svcCtx.BlacklistMap.Load(ip)

	if isBlacklisted {
		blacklistPopID := l.svcCtx.Config.Strategy.BlacklistPopId
		logx.Infof("node %s ip %s is in blacklist, redirect to blacklist pop %s", req.NodeId, ip, blacklistPopID)
		if pop, ok := l.svcCtx.Pops[blacklistPopID]; ok {
			return pop, nil
		}
	}

	// 2. Sticky allocation (keep existing pop if IP hasn't changed)
	popID, nodeIP, err := model.GetNodePopIP(l.svcCtx.Redis, req.NodeId)
	if err != nil {
		logx.Errorf("GetNodePopIP error: %v, node %s ip %s", err, req.NodeId, ip)
		return nil, err
	}

	if len(popID) > 0 && ip == string(nodeIP) {
		if pop, ok := l.svcCtx.Pops[string(popID)]; ok {
			logx.Debugf("node %s ip %s already exist pop %s", req.NodeId, nodeIP, popID)
			return pop, nil
		} else {
			logx.Errorf("node %s ip %s already exist pop %s, but pop not exist", req.NodeId, nodeIP, popID)
		}
	} else {
		logx.Debugf("node %s change ip %s to %s, old pop:%s, will re-allocate", req.NodeId, string(nodeIP), ip, popID)
	}

	// 3. Region Strategy (P2)
	location, err := l.getLocalInfo(ip)
	if err != nil {
		logx.Errorf("GetIPLocation error: %v, node %s, ip %s", err, req.NodeId)
		return nil, err
	}

	if pop, err := l.matchAndAllocatePop(l.svcCtx.RegionStrategy[location.Country], "region "+location.Country, req.NodeId, ip); err != nil {
		logx.Errorf("matchAndAllocatePop region error: %v", err)
		return nil, err
	} else if pop != nil {
		return pop, nil
	}

	// 4. Vendor Strategy (P3)
	if pop, err := l.matchAndAllocatePop(l.svcCtx.VendorStrategy[req.Vendor], "vendor "+req.Vendor, req.NodeId, ip); err != nil {
		logx.Errorf("matchAndAllocatePop vendor error: %v", err)
		return nil, err
	} else if pop != nil {
		return pop, nil
	}

	// 5. Default Strategy (P4)
	defaultPopID := l.svcCtx.Config.Strategy.DefaultPopId
	if pop, ok := l.svcCtx.Pops[defaultPopID]; ok {
		logx.Debugf("node %s allocate default pop:%s", req.NodeId, defaultPopID)
		l.saveNodePop(req.NodeId, defaultPopID, ip)
		return pop, nil
	}

	return nil, fmt.Errorf("no pop found for %s, location:%v, vendor:%s", req.NodeId, location, req.Vendor)
}

func (l *GetNodePopLogic) matchAndAllocatePop(popIds []string, strategyName, nodeID, ip string) (*svc.Pop, error) {
	if len(popIds) == 0 {
		return nil, nil
	}

	pop, err := l.selectPopFromList(l.ctx, popIds)
	if err != nil {
		return nil, err
	}

	if pop != nil {
		logx.Debugf("node %s matched %s, allocate pop:%s", nodeID, strategyName, pop.Config.Id)
		l.saveNodePop(nodeID, pop.Config.Id, ip)
		return pop, nil
	}

	return nil, fmt.Errorf("no pop found for %s, strategy: %s", nodeID, strategyName)
}

func (l *GetNodePopLogic) saveNodePop(nodeID, popID, ip string) {
	if err := model.SetNodePopIP(l.svcCtx.Redis, nodeID, popID, ip); err != nil {
		logx.Errorf("allocatePop SetNodePop error %v", err)
	}
}

func (l *GetNodePopLogic) selectPopFromList(ctx context.Context, popIds []string) (*svc.Pop, error) {
	if len(popIds) == 0 {
		return nil, nil
	}

	nodeCountMap, err := model.NodeCountOfPops(ctx, l.svcCtx.Redis, popIds)
	if err != nil {
		return nil, err
	}

	var bestPop *svc.Pop
	var minLoadRatio float64 = 2.0

	for _, id := range popIds {
		popEntity, ok := l.svcCtx.Pops[id]
		if !ok {
			continue
		}

		count := nodeCountMap[id]
		if count >= int64(popEntity.Config.MaxCount) && popEntity.Config.MaxCount > 0 {
			continue
		}

		loadRatio := 0.0
		if popEntity.Config.MaxCount > 0 {
			loadRatio = float64(count) / float64(popEntity.Config.MaxCount)
		}

		if loadRatio < minLoadRatio {
			minLoadRatio = loadRatio
			bestPop = popEntity
		}
	}
	return bestPop, nil
}

func (l *GetNodePopLogic) getLocalInfo(ip string) (*Location, error) {
	loc, err := model.GetIPLocation(l.svcCtx.Redis, ip)
	if err == nil {
		return &Location{IP: loc.IP, City: loc.City, Province: loc.Province, Country: loc.Country}, nil
	} else if err != redis.Nil {
		logx.Errorf("GetIPLocation error: %v", err)
	}

	v, err := l.svcCtx.IPGroup.Do(ip, func() (interface{}, error) {

		location, err := l.httpGetLocationInfo(ip)
		if err != nil {
			return nil, err
		}

		redisIPLocation := model.IPLocation{IP: location.IP, City: location.City, Province: location.Province, Country: location.Country}
		if err := model.SaveIPLocation(l.svcCtx.Redis, &redisIPLocation); err != nil {
			logx.Errorf("SaveIPLocation:%v", err)
		}

		return location, nil
	})

	if err != nil {
		return nil, err
	}

	return v.(*Location), nil
}

func (l *GetNodePopLogic) httpGetLocationInfo(ip string) (*Location, error) {
	logx.Infof("get %s local info from %s", ip, l.svcCtx.Config.GeoAPI.API)

	url := fmt.Sprintf("%s?ip=%s", l.svcCtx.Config.GeoAPI.API, ip)

	client := &http.Client{
		Timeout: 3 * time.Second,
	}

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

	if locationResp.Data == nil || len(locationResp.Data.Country) == 0 {
		return nil, fmt.Errorf("resp:%s", string(body))
	}

	location := locationResp.Data

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
