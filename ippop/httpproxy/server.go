package httpproxy

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/rest/httpx"
)

const (
	maxConnectionsPerIP = 5
)

// type TLSKeyPair struct {
// 	Cert string
// 	Key  string
// }

type Handle struct {
	routes      map[string]http.HandlerFunc
	tunMgr      *TunnelManager
	ipConnCount map[string]int
	ipConnLock  sync.Mutex
}

func newHandler(tunManager *TunnelManager) *Handle {
	h := &Handle{routes: make(map[string]http.HandlerFunc), tunMgr: tunManager, ipConnCount: make(map[string]int)}
	h.routes["/ws/web"] = h.handleWS
	h.routes["/web/crawler"] = h.handleWebCrawler
	return h
}

func (h *Handle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	handle := h.routes[path]
	if handle != nil {
		handle(w, r)
	} else {
		h.handleHttpProxy(w, r)
	}
}

func (h *Handle) handleWS(w http.ResponseWriter, r *http.Request) {
	ip, err := getRemoteIP(r)
	if err != nil {
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}

	h.ipConnLock.Lock()
	if h.ipConnCount[ip] >= maxConnectionsPerIP {
		h.ipConnLock.Unlock()
		httpx.ErrorCtx(r.Context(), w, fmt.Errorf("too many connections from ip %s", ip))
		return
	}
	h.ipConnCount[ip]++
	h.ipConnLock.Unlock()

	defer func() {
		h.ipConnLock.Lock()
		h.ipConnCount[ip]--
		if h.ipConnCount[ip] <= 0 {
			delete(h.ipConnCount, ip)
		}
		h.ipConnLock.Unlock()
	}()

	browserws := newBrowserWS(h.tunMgr)
	err = browserws.ServeWS(w, r)
	if err != nil {
		logx.Errorf("ServeWS error %v", err)
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}
}

func (h *Handle) handleHttpProxy(w http.ResponseWriter, r *http.Request) {
	httpProxy := newHttProxy(h.tunMgr)
	httpProxy.HandleProxy(w, r)
}

func (h *Handle) handleWebCrawler(w http.ResponseWriter, r *http.Request) {
	webCrawler := newWebCrawler(h.tunMgr)
	webCrawler.HandleWebCrawler(w, r)
}

type Server struct {
	addr    string
	server  *http.Server
	Handler http.Handler
	// router  *http.ServeMux
}

func NewServer(addr string, redisConf redis.RedisConf) *Server {
	redis := redis.MustNewRedis(redisConf)
	tunManager := NewTunnelManager(redis)
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
		logx.Infof("Starting HTTP server on %s", s.addr)
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

// func loadTLSConfig(keyPair *TLSKeyPair) (*tls.Config, error) {
// 	cert, err := tls.LoadX509KeyPair(keyPair.Cert, keyPair.Key)
// 	if err != nil {
// 		return nil, err
// 	}

// 	tlsConfig := &tls.Config{
// 		Certificates: []tls.Certificate{cert},
// 	}

// 	return tlsConfig, nil
// }
