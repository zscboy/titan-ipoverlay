package ws

import (
	"titan-ipoverlay/ippop/ws/pb"
)

type UploadTestStats struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Ongoing   int `json:"ongoing"`
}

func (tm *TunnelManager) SaveUploadTestResult(result *pb.UploadTestResult) {
	tm.uploadTestResults.Store(result.NodeId, result)
}

func (tm *TunnelManager) GetUploadTestResult(nodeID string) (*pb.UploadTestResult, bool) {
	v, ok := tm.uploadTestResults.Load(nodeID)
	if !ok {
		return nil, false
	}
	return v.(*pb.UploadTestResult), true
}

func (tm *TunnelManager) GetUploadTestResults(nodeIDs []string) []*pb.UploadTestResult {
	results := make([]*pb.UploadTestResult, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		if v, ok := tm.uploadTestResults.Load(id); ok {
			results = append(results, v.(*pb.UploadTestResult))
		}
	}
	return results
}

func (tm *TunnelManager) GetAllUploadTestResults() []*pb.UploadTestResult {
	results := make([]*pb.UploadTestResult, 0)
	tm.uploadTestResults.Range(func(key, value any) bool {
		results = append(results, value.(*pb.UploadTestResult))
		return true
	})
	return results
}

func (tm *TunnelManager) ClearUploadTestResults() {
	tm.uploadTestResults.Clear()
	tm.uploadTestTotal.Store(0)
}

func (tm *TunnelManager) SetUploadTestTotal(total int) {
	tm.uploadTestTotal.Store(int32(total))
}

func (tm *TunnelManager) GetUploadTestStats() *UploadTestStats {
	stats := &UploadTestStats{
		Total: int(tm.uploadTestTotal.Load()),
	}

	tm.uploadTestResults.Range(func(key, value any) bool {
		res := value.(*pb.UploadTestResult)
		stats.Completed++
		if !res.Success {
			stats.Failed++
		}
		return true
	})

	stats.Ongoing = stats.Total - stats.Completed
	if stats.Ongoing < 0 {
		stats.Ongoing = 0
	}
	return stats
}

func (tm *TunnelManager) GetTunnelsFromPool(count int) []string {
	if count <= 0 {
		return nil
	}
	freeIPs := tm.ipPool.GetFreeIPsFromHead(count)
	nodeIDs := make([]string, 0, len(freeIPs))
	for _, info := range freeIPs {
		if len(info.NodeIDs) > 0 {
			nodeIDs = append(nodeIDs, info.NodeIDs[0]) // just take the first tunnel for this IP
		}
	}
	return nodeIDs
}
