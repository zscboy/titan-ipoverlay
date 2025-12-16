package yuanren

import (
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
	"titan-ipoverlay/manager/internal/logic/yuanren"
	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"
)

func StartOrStopUserHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.StartOrStopUserReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := yuanren.NewStartOrStopUserLogic(r.Context(), svcCtx)
		resp, err := l.StartOrStopUser(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
