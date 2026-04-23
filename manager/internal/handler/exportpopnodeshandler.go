package handler

import (
	"net/http"

	"titan-ipoverlay/manager/internal/logic"
	"titan-ipoverlay/manager/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

func exportPopNodesHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewExportPopNodesLogic(r.Context(), svcCtx)
		if err := l.ExportPopNodesCSV(w); err != nil {
			logx.Errorf("ExportPopNodesCSV error: %v", err)
		}
	}
}
