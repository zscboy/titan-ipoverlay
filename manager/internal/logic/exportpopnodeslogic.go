package logic

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"

	"titan-ipoverlay/manager/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type ExportPopNodesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewExportPopNodesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ExportPopNodesLogic {
	return &ExportPopNodesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// ExportPopNodesCSV streams all POP node IDs as a CSV download.
// Format: pop_id, pop_name, node_id
// Uses SSCAN internally so it won't block Redis.
func (l *ExportPopNodesLogic) ExportPopNodesCSV(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=pop_nodes.csv")

	// Get the Flusher interface to push data to client immediately
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write CSV header
	if err := writer.Write([]string{"pop_id", "pop_name", "node_id"}); err != nil {
		return err
	}
	writer.Flush()
	flusher.Flush() // Push header to client immediately

	for id, pop := range l.svcCtx.Pops {
		popName := pop.Config.Name

		// Stream node IDs using SSCAN (no full load into memory)
		key := fmt.Sprintf("titan:manager:pop:%s", id)
		var cursor uint64

		for {
			ids, nextCursor, err := l.svcCtx.Redis.Sscan(key, cursor, "", 1000)
			if err != nil {
				logx.Errorf("ExportPopNodesCSV: sscan pop %s error: %v", id, err)
				return err
			}

			for _, nodeID := range ids {
				if err := writer.Write([]string{id, popName, nodeID}); err != nil {
					return err
				}
			}

			// Flush CSV buffer to ResponseWriter, then flush HTTP to client
			writer.Flush()
			flusher.Flush()

			cursor = nextCursor
			if cursor == 0 {
				break
			}
		}
	}

	return nil
}
