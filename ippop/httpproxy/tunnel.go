package httpproxy

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	uuidLength = 32
)

type TunOptions struct {
	Id string
	// OS string
	IP string
}

// Tunnel Tunnel
type Tunnel struct {
	conn      *websocket.Conn
	writeLock sync.Mutex
	waitping  int
	opts      *TunOptions
	reqs      sync.Map
	tunMgr    *TunnelManager
}

func newTunnel(conn *websocket.Conn, tunMgr *TunnelManager, opts *TunOptions) *Tunnel {

	t := &Tunnel{
		conn:   conn,
		opts:   opts,
		tunMgr: tunMgr,
	}

	conn.SetPingHandler(func(data string) error {
		t.writePong([]byte(data))
		return nil
	})

	conn.SetPongHandler(func(data string) error {
		t.onPong()
		return nil
	})

	return t
}

func (t *Tunnel) writePong(msg []byte) error {
	t.writeLock.Lock()
	defer t.writeLock.Unlock()
	return t.conn.WriteMessage(websocket.PongMessage, msg)
}

func (t *Tunnel) writePing(msg []byte) error {
	t.writeLock.Lock()
	defer t.writeLock.Unlock()
	return t.conn.WriteMessage(websocket.PingMessage, msg)
}

func (t *Tunnel) onPong() {
	t.waitping = 0
}

func (t *Tunnel) serve() {
	defer logx.Debugf("Tunnel %s %s close", t.opts.Id, t.opts.IP)
	defer t.close()

	c := t.conn
	for {
		messageType, message, err := c.ReadMessage()
		if err != nil {
			logx.Error("Tunnel read failed:", err)
			return
		}

		if messageType == websocket.TextMessage {
			if err := t.onTextMessage(message); err != nil {
				logx.Errorf("Tunnel.serve onTextMessage failed:%v", err.Error())
			}
		} else if messageType == websocket.BinaryMessage {
			if err := t.onBinaryMessage(message); err != nil {
				logx.Error("Tunnel.serve onMessage failed:", err)
			}
		} else {
			logx.Errorf("unsupport websocket message type:%d", messageType)
		}
	}
}

func (t *Tunnel) onTextMessage(data []byte) error {
	logx.Debugf("onTextMessage %s", string(data))
	msg := &Message{}
	err := json.Unmarshal(data, msg)
	if err != nil {
		return err
	}

	if msg.Type != ClientResponseHeaders && msg.Type != ClientResponseError {
		return fmt.Errorf("invalid message type")
	}

	switch msg.Type {
	case ClientResponseHeaders:
		return t.handleClientResponseHeaders(msg)
	case ClientResponseError:
		return t.handleClientResponseErr(msg)
	default:
		return fmt.Errorf("unsupport msg type %d", msg.Type)
	}

}

func (t *Tunnel) handleClientResponseHeaders(msg *Message) error {
	payloadMap, ok := msg.Payload.(map[string]interface{})
	if !ok {
		return fmt.Errorf("handleClientResponseHeaders, payload not map")
	}

	payloadBytes, err := json.Marshal(payloadMap)
	if err != nil {
		return err
	}

	var response HTTPResponseHeader
	err = json.Unmarshal(payloadBytes, &response)
	if err != nil {
		return err
	}

	headerBuilder := response.rebuildHTTPHeaders()
	logx.Debugf("header:%s", headerBuilder.String())
	contentLength, err := getContentLength(response.Header)
	if err != nil {
		return err
	}

	t.reqs.Range(func(key, value any) bool {
		logx.Debugf("foreach reqs %s %v", key.(string), value)
		return true
	})

	v, ok := t.reqs.Load(response.ID)
	if !ok {
		logx.Errorf("Tunnel.handleClientResponseHeaders tunnel:%s, http request %s not exist", t.opts.Id, response.ID)
		return fmt.Errorf("http request %s not exist", response.ID)
	}

	req := v.(*Req)
	req.rspContentLength = contentLength
	if err := req.write([]byte(headerBuilder.String())); err != nil {
		return err
	}

	logx.Debugf("handleClientResponseHeaders contentLength:%d", contentLength)
	if contentLength == 0 {
		req.close()
	}

	return nil
}

func (t *Tunnel) handleClientResponseErr(msg *Message) error {
	payloadMap, ok := msg.Payload.(map[string]interface{})
	if !ok {
		return fmt.Errorf("handleClientResponseHeaders, payload not map")
	}

	payloadBytes, err := json.Marshal(payloadMap)
	if err != nil {
		return err
	}

	var response HTTPResponseError
	err = json.Unmarshal(payloadBytes, &response)
	if err != nil {
		return err
	}

	v, ok := t.reqs.Load(response.ID)
	if !ok {
		return fmt.Errorf("http request %s not exist", response.ID)
	}
	req := v.(*Req)
	req.close()

	return nil
	// headerBuilder := response.rebuildHTTPHeaders()
	// logx.Debugf("header:%s", headerBuilder.String())
	// contentLength, err := getContentLength(response.Header)
	// if err != nil {
	// 	return err
	// }
}

func (t *Tunnel) onBinaryMessage(data []byte) error {
	if len(data) <= uuidLength {
		return fmt.Errorf("invalid data length %d < %d", len(data), uuidLength)
	}
	id := string(data[:uuidLength])

	v, ok := t.reqs.Load(id)
	if !ok {
		return fmt.Errorf("can not find request %s, content length:%d", id, len(data[uuidLength:]))
	}

	// logx.Debugf("data:%s", string(data[uuidLength:]))
	// logx.Debugf("onBinaryMessage data length:%d", len(data[uuidLength:]))

	req := v.(*Req)
	req.rspDataLength = req.rspDataLength + int64(len(data[uuidLength:]))
	req.write(data[uuidLength:])

	if req.rspDataLength >= req.rspContentLength {
		req.close()
		t.reqs.Delete(id)
		logx.Debugf("onBinaryMessage %s recive complete", id)
	}
	return nil
}

func (t *Tunnel) onHTTPRequest(targetInfo *TargetInfo) error {
	defer logx.Debugf("onHTTPRequest complete")
	req := &Req{ID: targetInfo.req.ID, conn: targetInfo.conn, tun: t}

	t.reqs.Store(req.ID, req)
	defer t.reqs.Delete(req.ID)

	msg := &Message{Type: ServerRequest, Payload: targetInfo.req}

	reqData, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	logx.Debugf("onHTTPRequest: tunnel:%s, req:%s", t.opts.Id, string(reqData))

	if err := t.writeText(reqData); err != nil {
		return err
	}

	return req.proxy()
}

func (t *Tunnel) onHTTPData(id string, data []byte) error {
	uid := uuid.MustParse(id)
	buf := append(uid[:], data...)
	return t.writeBinary(buf)
}

func (t *Tunnel) keepalive() {
	if t.waitping > 3 {
		t.conn.Close()
		return
	}

	b := make([]byte, 8)
	now := time.Now().Unix()
	binary.LittleEndian.PutUint64(b, uint64(now))

	t.writePing(b)

	t.waitping++
}

func (t *Tunnel) writeBinary(msg []byte) error {
	t.writeLock.Lock()
	defer t.writeLock.Unlock()

	return t.conn.WriteMessage(websocket.BinaryMessage, msg)
}

func (t *Tunnel) writeText(msg []byte) error {
	t.writeLock.Lock()
	defer t.writeLock.Unlock()

	return t.conn.WriteMessage(websocket.TextMessage, msg)
}

func (t *Tunnel) close() {
	if t.conn != nil {
		t.conn.Close()
	}

}
