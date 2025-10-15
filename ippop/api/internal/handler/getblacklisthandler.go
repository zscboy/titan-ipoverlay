package handler

import (
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
	"titan-ipoverlay/ippop/api/internal/logic"
	"titan-ipoverlay/ippop/api/internal/svc"
)

func getBlackListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewGetBlackListLogic(r.Context(), svcCtx)
		resp, err := l.GetBlackList()
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
