package ws

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"titan-ipoverlay/ippop/model"
	"titan-ipoverlay/ippop/ws/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
)

type UploadTestReq struct {
	NodeIDs  []string `json:"node_ids"`
	Duration int      `json:"duration"` // seconds
}

type UploadTestResp struct {
	Stats   *UploadTestStats       `json:"stats,omitempty"`
	Results []*pb.UploadTestResult `json:"results"`
}

type UploadTestHandler struct {
	tunMgr *TunnelManager
}

func NewUploadTestHandler(tunMgr *TunnelManager) *UploadTestHandler {
	return &UploadTestHandler{tunMgr: tunMgr}
}

func (h *UploadTestHandler) ServeUploadTest(w http.ResponseWriter, r *http.Request) {
	var req UploadTestReq
	if err := httpx.Parse(r, &req); err != nil {
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}

	if len(req.NodeIDs) == 0 {
		httpx.ErrorCtx(r.Context(), w, fmt.Errorf("node_ids is required"))
		return
	}

	if req.Duration <= 0 {
		req.Duration = 10
	}

	// Limit max batch size to 2000
	if len(req.NodeIDs) > 2000 {
		httpx.ErrorCtx(r.Context(), w, fmt.Errorf("too many node_ids, max 2000"))
		return
	}

	// Check if another test is in progress
	stats := h.tunMgr.GetUploadTestStats()
	if stats.Ongoing > 0 {
		httpx.ErrorCtx(r.Context(), w, fmt.Errorf("another upload test is in progress: %d/%d completed, %d ongoing",
			stats.Completed, stats.Total, stats.Ongoing))
		return
	}

	// Clear old results and set new total
	h.tunMgr.ClearUploadTestResults()
	h.tunMgr.SetUploadTestTotal(len(req.NodeIDs))

	// Async start tests
	go func() {
		nodeResults := make(map[string]float64)
		var mu sync.Mutex
		var wg sync.WaitGroup

		for _, nodeID := range req.NodeIDs {
			tun := h.tunMgr.GetLocalTunnel(nodeID)
			if tun == nil {
				h.tunMgr.SaveUploadTestResult(&pb.UploadTestResult{
					NodeId:   nodeID,
					Success:  false,
					ErrorMsg: "node offline locally",
				})
				continue
			}

			wg.Add(1)
			// Individual test in its own goroutine
			go func(t *Tunnel) {
				defer wg.Done()

				result, err := t.StartUploadTest(context.Background(), req.Duration)
				if err != nil {
					h.tunMgr.SaveUploadTestResult(&pb.UploadTestResult{
						NodeId:   t.opts.Id,
						Success:  false,
						ErrorMsg: err.Error(),
					})
					return
				}

				// If success, collect for batch save
				if result.Success {
					mu.Lock()
					nodeResults[result.NodeId] = result.Mbps
					mu.Unlock()
				}

				h.tunMgr.SaveUploadTestResult(result)
			}(tun)
		}

		// Wait for all tests in this batch to finish
		wg.Wait()

		// Perform batch save to Redis
		if len(nodeResults) > 0 {
			if err := model.SetNodesBandwidth(context.Background(), h.tunMgr.redis, nodeResults); err != nil {
				logx.Errorf("batch save node bandwidth failed: %v", err)
			} else {
				logx.Infof("batch save %d node bandwidth results success", len(nodeResults))
			}
		}
	}()

	httpx.OkJsonCtx(r.Context(), w, map[string]string{"status": "test_initiated", "count": fmt.Sprintf("%d", len(req.NodeIDs))})
}

type UploadTestResultReq struct {
	NodeIDs []string `form:"node_ids,optional"`
	Clear   bool     `form:"clear,optional"`
}

func (h *UploadTestHandler) ServeUploadTestResult(w http.ResponseWriter, r *http.Request) {
	var req UploadTestResultReq
	if err := httpx.Parse(r, &req); err != nil {
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}

	if req.Clear {
		h.tunMgr.ClearUploadTestResults()
		httpx.OkJsonCtx(r.Context(), w, map[string]string{"status": "cleared"})
		return
	}

	httpx.OkJsonCtx(r.Context(), w, &UploadTestResp{
		Stats:   h.tunMgr.GetUploadTestStats(),
		Results: h.tunMgr.GetUploadTestResults(req.NodeIDs),
	})
}
