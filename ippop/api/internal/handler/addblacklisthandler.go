package handler

import (
	"net/http"

	"titan-ipoverlay/ippop/api/internal/logic"
	"titan-ipoverlay/ippop/api/internal/svc"
	"titan-ipoverlay/ippop/api/internal/types"

	"github.com/zeromicro/go-zero/rest/httpx"
)

func addBlackListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AddBlackListReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := logic.NewAddBlackListLogic(r.Context(), svcCtx)
		err := l.AddBlackList(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.Ok(w)
		}
	}
}
