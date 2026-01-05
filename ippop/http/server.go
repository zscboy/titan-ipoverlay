package http

import (
	"context"
	"log"
	"net/http"
	"titan-ipoverlay/ippop/ws"

	"github.com/zeromicro/go-zero/core/logx"
)

type Handler struct {
	httpproxy *HttpProxy
}

func newHandler(tunManager *ws.TunnelManager) *Handler {
	return &Handler{httpproxy: NewHttProxy(tunManager)}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	logx.Debugf("ServeHTTP, path:%s", r.URL)
	h.httpproxy.HandleProxy(w, r)
}

type Server struct {
	addr    string
	server  *http.Server
	Handler http.Handler
}

func NewServer(addr string, tunManager *ws.TunnelManager) *Server {
	return &Server{addr: addr, Handler: newHandler(tunManager)}
}

func (s *Server) Start() {
	if s.server != nil {
		logx.Error("server already start")
		return
	}

	s.server = &http.Server{
		Addr:    s.addr,
		Handler: s.Handler,
	}

	go func() {
		logx.Infof("Starting http proxy server on %s", s.addr)
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

}

func (s *Server) Stop() {
	if s.server == nil {
		logx.Error("server not start")
		return
	}

	log.Println("Shutting down server...")
	if err := s.server.Shutdown(context.TODO()); err != nil {
		logx.Error("server shutdown failed: %v", err)
	}

}
