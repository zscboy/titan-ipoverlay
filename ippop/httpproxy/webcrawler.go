package httpproxy

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"titan-ipoverlay/ippop/api/model"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// const (
// 	anonymousUserName = "anonymous"
// 	maxBodySize       = 8 << 20 // 8MB
// )

type WebCrawler struct {
	tunMgr *TunnelManager
}

func newWebCrawler(tunMgr *TunnelManager) *WebCrawler {
	return &WebCrawler{tunMgr: tunMgr}
}

type URLReq struct {
	URL string `form:"url"`
}

func (webCrawler *WebCrawler) HandleWebCrawler(w http.ResponseWriter, r *http.Request) {
	var urlReq URLReq
	if err := httpx.Parse(r, &urlReq); err != nil {
		http.Error(w, fmt.Sprintf("Paser paras failed:%s", err.Error()), http.StatusBadRequest)
		return
	}

	logx.Debugf("url:%s", urlReq.URL)

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
		URL:    url.QueryEscape(urlReq.URL),
		Header: r.Header,
	}

	userName := anonymousUserName
	targetInfo := &TargetInfo{conn: clientConn, req: &req, userName: userName}

	if err := webCrawler.tunMgr.onHTTPRequest(targetInfo); err != nil {
		logx.Errorf("onHTTPRequest %v", err)
	}
}

// return userName
func (webCrawler *WebCrawler) checkAuth(auth string) ([]byte, error) {
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

	user, err := model.GetUser(webCrawler.tunMgr.redis, userName)
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
