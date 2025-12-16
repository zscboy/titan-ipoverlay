package yuanren

import (
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
	"titan-ipoverlay/manager/internal/logic/yuanren"
	"titan-ipoverlay/manager/internal/svc"
)

func GetPopsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := yuanren.NewGetPopsLogic(r.Context(), svcCtx)
		resp, err := l.GetPops()
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
