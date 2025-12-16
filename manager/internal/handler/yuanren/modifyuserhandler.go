package yuanren

import (
	"net/http"

	"titan-ipoverlay/manager/internal/logic/yuanren"
	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"

	"github.com/zeromicro/go-zero/rest/httpx"
)

func ModifyUserHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ModifyUserReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := yuanren.NewModifyUserLogic(r.Context(), svcCtx)
		err := l.ModifyUser(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.Ok(w)
		}
	}
}
