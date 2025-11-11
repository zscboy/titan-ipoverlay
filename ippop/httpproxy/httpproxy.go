package httpproxy

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"titan-ipoverlay/ippop/model"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	anonymousUserName = "anonymous"
	maxBodySize       = 8 << 20 // 8MB
)

type HttpProxy struct {
	tunMgr *TunnelManager
}

func newHttProxy(tunMgr *TunnelManager) *HttpProxy {
	return &HttpProxy{tunMgr: tunMgr}
}

func (httpProxy *HttpProxy) HandleProxy(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

	// auth := r.Header.Get("Proxy-Authorization")
	// userNameBytes, err := httpProxy.checkAuth(auth)
	// if err != nil {
	// 	logx.Errorf("check auth %v", err.Error())
	// 	w.Header().Set("Proxy-Authenticate", `Basic realm="Restricted"`)
	// 	w.WriteHeader(http.StatusProxyAuthRequired)
	// 	_, _ = w.Write([]byte("407 Proxy Authentication Required\n"))
	// 	return
	// }

	// userName := string(userNameBytes)
	userName := anonymousUserName

	if r.Method == http.MethodConnect {
		httpProxy.handleHTTPS(w, r, userName)
	} else {
		httpProxy.handleHTTP(w, r, userName)
	}
}

func (httpProxy *HttpProxy) handleHTTPS(w http.ResponseWriter, r *http.Request, userName string) {
	http.Error(w, "not support https now", http.StatusInternalServerError)
	// hij, ok := w.(http.Hijacker)
	// if !ok {
	// 	http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
	// 	return
	// }

	// client, _, err := hij.Hijack()
	// if err != nil {
	// 	http.Error(w, err.Error(), http.StatusServiceUnavailable)
	// 	return
	// }
	// defer client.Close()

	// _, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	// tlsConn := tls.Server(client)
	// defer tlsConn.Close()

	// reader := bufio.NewReader(tlsConn)
	// req, err := http.ReadRequest(reader)
	// if err != nil {
	// 	log.Println("ReadRequest:", err)
	// 	return
	// }
	// defer req.Body.Close()

	// log.Println("Method:", req.Method)
	// log.Println("Headers:", req.Header)

	// body, err := io.ReadAll(req.Body)
	// if err != nil {
	// 	log.Println("读取请求体失败:", err)
	// 	return
	// }
	// log.Println("Body:", string(body))

	// target := r.Host
	// newReq, err := http.NewRequest(req.Method, "https://"+target, bytes.NewReader(body))
	// if err != nil {
	// 	log.Println("构造新请求失败:", err)
	// 	return
	// }
	// newReq.Header = req.Header

	// resp, err := http.DefaultClient.Do(newReq)
	// if err != nil {
	// 	log.Println("请求目标服务器失败:", err)
	// 	return
	// }
	// defer resp.Body.Close()

	// resp.Write(tlsConn)
}

func (httpProxy *HttpProxy) handleHTTP(w http.ResponseWriter, r *http.Request, userName string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	r.Body.Close()

	hij, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hij.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer clientConn.Close()

	uid := uuid.New()
	req := HTTPRequest{
		ID:     hex.EncodeToString(uid[:]),
		Method: r.Method,
		URL:    r.URL.String(),
		Header: r.Header,
	}

	if len(body) > 0 {
		req.Body = body
	}

	targetInfo := &TargetInfo{conn: clientConn, req: &req, userName: userName}

	if err := httpProxy.tunMgr.onHTTPRequest(targetInfo); err != nil {
		logx.Errorf("onHTTPRequest %v", err)
	}
}

// return userName
func (httpProxy *HttpProxy) checkAuth(auth string) ([]byte, error) {
	if !strings.HasPrefix(auth, "Basic ") {
		return nil, fmt.Errorf("client not include Proxy-Authorization Basic ")
	}
	payload, err := base64.StdEncoding.DecodeString(auth[len("Basic "):])
	if err != nil {
		return nil, fmt.Errorf("decode Basic failed:%v", err.Error())
	}
	pair := strings.SplitN(string(payload), ":", 2)
	if len(pair) != 2 {
		return nil, fmt.Errorf("invalid user and password")
	}

	userName := pair[0]
	if len(userName) == 0 {
		return nil, fmt.Errorf("invalid user")
	}

	user, err := model.GetUser(httpProxy.tunMgr.redis, userName)
	if err != nil {
		return nil, fmt.Errorf("get user failed:%v", err.Error())
	}

	if user == nil {
		return nil, fmt.Errorf("user %s not exist", userName)
	}

	password := pair[1]
	if len(password) == 0 {
		return nil, fmt.Errorf("invalid password")
	}

	hash := md5.Sum([]byte(password))
	passwordMD5 := hex.EncodeToString(hash[:])
	if passwordMD5 != user.PasswordMD5 {
		return nil, fmt.Errorf("password not match")
	}

	return []byte(userName), nil
}
