package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type PopStats struct {
	NodeCount        int `json:"node_count"`
	TotalIPCount     int `json:"total_ip_count"`
	FreeIPCount      int `json:"free_ip_count"`
	BlacklistIPCount int `json:"blacklist_ip_count"`
	AssignedIPCount  int `json:"assigned_ip_count"`
	UserSessionCount int `json:"user_session_count"`
	UserConnCount    int `json:"user_conn_count"`
}

type PopMonitorLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPopMonitorLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PopMonitorLogic {
	return &PopMonitorLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *PopMonitorLogic) PopMonitor(req *types.PopMonitorReq) (resp *types.PopMonitorResp, err error) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var items []*types.PopMonitorItem

	targetPops := make(map[string]bool)
	for _, id := range req.PopIDs {
		targetPops[id] = true
	}

	for id, pop := range l.svcCtx.Pops {
		if targetPops[id] {
			continue // Exclude POPs specified in the filter list
		}
		wg.Add(1)
		go func(popID string, popEntity *svc.Pop) {
			defer wg.Done()

			statsURL := getPopStatsURL(popEntity.Config.WSURL)
			item := &types.PopMonitorItem{
				ID:   popID,
				Name: popEntity.Config.Name,
			}

			popStats, err := l.fetchPopStats(statsURL)
			if err != nil {
				l.Errorf("PopMonitorLogic: Failed to fetch stats from POP %s (%s): %v", popID, statsURL, err)
			} else {
				item.UsedIPCount = popStats.AssignedIPCount
				item.IdleIPCount = popStats.FreeIPCount
				if popStats.TotalIPCount > 0 {
					item.IdleIPRatio = (float64(popStats.FreeIPCount) / float64(popStats.TotalIPCount)) * 100.0
				} else {
					item.IdleIPRatio = 0.0
				}
			}

			mu.Lock()
			items = append(items, item)
			mu.Unlock()
		}(id, pop)
	}

	wg.Wait()

	// Filter items in memory based on threshold and operator
	var filtered []*types.PopMonitorItem
	if req.IdleOperator != "" {
		for _, item := range items {
			match := false
			switch req.IdleOperator {
			case "lt":
				match = item.IdleIPRatio < req.IdleThreshold
			case "gt":
				match = item.IdleIPRatio > req.IdleThreshold
			case "lte":
				match = item.IdleIPRatio <= req.IdleThreshold
			case "gte":
				match = item.IdleIPRatio >= req.IdleThreshold
			default:
				match = true
			}
			if match {
				filtered = append(filtered, item)
			}
		}
	} else {
		filtered = items
	}

	return &types.PopMonitorResp{Pops: filtered}, nil
}

func (l *PopMonitorLogic) fetchPopStats(url string) (*PopStats, error) {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		stats, err := l.doFetchPopStats(url)
		if err == nil {
			return stats, nil
		}

		lastErr = err

		// Check if it's a timeout error
		isTimeout := false
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			isTimeout = true
		} else if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline exceeded") {
			isTimeout = true
		}

		// If it's not a timeout, return immediately (e.g. 404, connection refused)
		if !isTimeout {
			return nil, err
		}

		l.Errorf("PopMonitorLogic: Timeout on attempt %d for url %s, retrying... (Error: %v)", attempt, url, err)
		time.Sleep(100 * time.Millisecond)
	}
	return nil, fmt.Errorf("failed after 3 attempts: %w", lastErr)
}

func (l *PopMonitorLogic) doFetchPopStats(url string) (*PopStats, error) {
	ctx, cancel := context.WithTimeout(l.ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := l.svcCtx.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bs, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP status %d, response: %s", resp.StatusCode, string(bs))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var stats PopStats
	if err := json.Unmarshal(body, &stats); err != nil {
		return nil, err
	}

	return &stats, nil
}

func getPopStatsURL(wsURL string) string {
	u := wsURL
	u = strings.Replace(u, "ws://", "http://", 1)
	u = strings.Replace(u, "wss://", "https://", 1)
	idx := strings.Index(u, "/ws/node")
	if idx != -1 {
		u = u[:idx]
	}
	return u + "/pop/stats"
}
