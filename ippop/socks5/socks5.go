package socks5

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

type Socks5Handler interface {
	HandleSocks5TCP(*net.TCPConn, *SocksTargetInfo) error
	HandleSocks5UDP(udpConn UDPConn, udpInfo *Socks5UDPInfo, data []byte) error
	HandleUserAuth(userName, password string) error
}

type Socks5ServerOptions struct {
	Address      string
	UDPServerIP  string
	UDPPortStart int
	UDPPortEnd   int
	EnableAuth   bool
	// UseBypass bool
	Handler Socks5Handler
	// TransportHandler meta.HTTPSocks5TransportHandler
	// BypassHandler    meta.Bypass
}

type SocksTargetInfo struct {
	DomainName string
	Port       int

	ExtraBytes []byte
	UserName   string
}

type UDPConn interface {
	WriteToUDP(b []byte, addr *net.UDPAddr) (int, error)
}

type Socks5UDPInfo struct {
	UDPServerID string
	Src         string
	Dest        string
	UserName    string
}

type Socks5Server struct {
	opts           *Socks5ServerOptions
	userUDPServers sync.Map
	listener       *net.TCPListener
	userIPCount    *userIPCount
	userUDPCount   *userUDPCount
}

func New(opts *Socks5ServerOptions) (*Socks5Server, error) {
	if len(opts.Address) == 0 {
		return nil, fmt.Errorf("must set option Address")
	}

	if opts.Handler == nil {
		return nil, fmt.Errorf("must set option Handler")
	}

	socks5Server := &Socks5Server{
		opts:        opts,
		userIPCount: newUserIPCount(),
	}

	userUDPCount := newUserUDPCount(socks5Server)
	socks5Server.userUDPCount = userUDPCount

	return socks5Server, nil
}

func (socks5Server *Socks5Server) Start() {
	if socks5Server.listener != nil {
		panic("localsocks5.Socks5Server already startup")
		// return fmt.Errorf("localsocks5.Socks5Server already startup")
	}

	var err error
	addr, err := net.ResolveTCPAddr("tcp", socks5Server.opts.Address)
	if err != nil {
		// return fmt.Errorf("localsocks5.Socks5Server resolve tcp address error:%s", err)
		panic(fmt.Errorf("localsocks5.Socks5Server resolve tcp address error:%s", err))
	}

	socks5Server.listener, err = net.ListenTCP("tcp", addr)
	if err != nil {
		// return fmt.Errorf("localsocks5.Socks5Server ListenTCP error:%s", err)
		panic(fmt.Errorf("localsocks5.Socks5Server ListenTCP error:%s", err))
	}

	logx.Infof("Socks5 server start at:%s", socks5Server.opts.Address)
	go socks5Server.serveSocks5()

	// return nil
}

func (socks5Server *Socks5Server) Stop() {
	if socks5Server.listener == nil {
		// return fmt.Errorf("localsocks5.Socks5Server isn't running")
		panic("localsocks5.Socks5Server isn't running")
	}

	err := socks5Server.listener.Close()
	if err != nil {
		logx.Errorf("localsocks5.Socks5Server shutdown TCP server failed:%s", err)
	}

	logx.Info("Socks5 server shutdown")
	// return nil
}

func (socks5Server *Socks5Server) serveSocks5() {
	for {
		conn, err := socks5Server.listener.Accept()
		if err != nil {
			logx.Errorf("localsocks5.Socks5Server serveSocks5 error:%s", err)
			return
		}

		go socks5Server.serveSocks5Conn(conn)
	}
}

func (socks5Server *Socks5Server) serveSocks5Conn(conn net.Conn) {
	logx.Debug("Socks5Server.serveSocks5Conn")
	var handled = false
	defer func() {
		if r := recover(); r != nil {
			logx.Errorf("serveSocks5Conn Recovered. Error:%s", r)
		}

		if !handled {
			conn.Close()

			logx.Debug("Socks5Server.serveSocks5Conn conn close")
		}
	}()

	bufConn := bufio.NewReader(conn)

	// Read the version byte
	version := []byte{0}
	if _, err := bufConn.Read(version); err != nil {
		logx.Errorf("Socks5Server.serveSocks5Conn failed to get socks5 version byte: %v", err)
		return
	}

	// Ensure we are compatible
	if version[0] != socks5Version {
		logx.Errorf("Socks5Server.serveSocks5Conn Unsupported SOCKS version: %v", version)
		return
	}

	var user, err = socks5Server.authenticate(conn, bufConn)
	if err != nil {
		logx.Errorf("Socks5Server.serveSocks5Conn auth failed: %v", err)
		return
	}

	r1, err := newRequest(bufConn, conn)
	if err != nil {
		logx.Errorf("Socks5Server.serveSocks5Conn newRequest error: %v", err)
		return
	}

	if user != nil {
		r1.user = string(user)
	}

	err = socks5Server.handleSocks5Request(r1)
	if err != nil {
		logx.Errorf("Socks5Server.serveSocks5Conn handleSocks5Request error: %v", err)
		return
	}

	handled = true
}

func (socks5Server *Socks5Server) authenticate(conn io.Writer, bufConn io.Reader) ([]byte, error) {
	// Get the methods
	methods, err := readAuthMethods(bufConn)
	if err != nil {
		return nil, fmt.Errorf("failed to get auth methods: %v", err)
	}

	authMethod := noAuth
	if socks5Server.opts.EnableAuth {
		authMethod = userPassAuth
	}

	got := false
	for _, method := range methods {
		if method == authMethod {
			got = true
			break
		}
	}

	if !got {
		return nil, noAcceptableAuth(conn)
	}

	if authMethod == userPassAuth {
		return socks5Server.userPassAuth(conn, bufConn)
	}

	return nil, noAuthAuthenticator{}.authenticate(conn)
}

// userPassAuth return user
func (socks5Server *Socks5Server) userPassAuth(writer io.Writer, reader io.Reader) ([]byte, error) {
	// Tell the client to use user/pass auth
	if _, err := writer.Write([]byte{socks5Version, userPassAuth}); err != nil {
		return nil, err
	}

	// Get the version and username length
	header := []byte{0, 0}
	if _, err := io.ReadAtLeast(reader, header, 2); err != nil {
		return nil, err
	}

	// Ensure we are compatible
	if header[0] != userAuthVersion {
		return nil, fmt.Errorf("unsupported auth version: %v", header[0])
	}

	// Get the user name
	userLen := int(header[1])
	user := make([]byte, userLen)
	if _, err := io.ReadAtLeast(reader, user, userLen); err != nil {
		return nil, err
	}

	// Get the password length
	if _, err := reader.Read(header[:1]); err != nil {
		return nil, err
	}

	// Get the password
	passLen := int(header[0])
	pass := make([]byte, passLen)
	if _, err := io.ReadAtLeast(reader, pass, passLen); err != nil {
		return nil, err
	}

	// Verify the password
	if err := socks5Server.opts.Handler.HandleUserAuth(string(user), string(pass)); err != nil {
		logx.Errorf("Socks5Server.userPassAuth HandleUserAuth failed:%v", err.Error())
		return nil, userPassAuthFailure(writer)
	}

	if err := userPassAuthSuccess(writer); err != nil {
		return nil, err
	}

	return user, nil
}

func (socks5Server *Socks5Server) handleSocks5Request(r *request) error {
	// switch on the command
	switch r.command {
	case connectCommand:
		return socks5Server.handleSocks5Connect(r)
	case bindCommand:
		return socks5Server.handleSocks5Bind(r)
	case associateCommand:
		return socks5Server.handleSocks5Associate(r)
	default:
		if err := replySocks5Client(r.conn, commandNotSupported, nil); err != nil {
			return fmt.Errorf("failed to send reply: %v", err)
		}
		return fmt.Errorf("unsupported command: %v", r.command)
	}
}

// NOTE: if error occurs, conn of the 'req' object must be closed in outer
func (socks5Server *Socks5Server) handleSocks5Connect(req *request) error {
	if ip := net.ParseIP(req.destAddr.fqdn); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsMulticast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("Socks5Server.handleSocks5Connect not support ip %s", ip.String())
		}
	}

	var extraBytes []byte

	if req.bufreader != nil {
		buffered := req.bufreader.Buffered()
		if buffered > 0 {
			extraBytes = make([]byte, buffered)
			n, err := req.bufreader.Read(extraBytes)
			if err != nil {
				return err
			}

			if n != buffered {
				return fmt.Errorf("handleConnect drain bufreader failed:%s", err)
			}
		}
	}

	// cfg := socks5Server.opts
	targetInfo := &SocksTargetInfo{
		Port:       req.destAddr.port,
		DomainName: req.destAddr.fqdn,
		ExtraBytes: extraBytes,
		UserName:   req.user,
	}

	tcpConn, ok := req.conn.(*net.TCPConn)
	if !ok {
		return fmt.Errorf("handleConnect socks5 conn isn't tcp conn")
	}

	tcpConn.SetKeepAlivePeriod(5 * time.Second)
	tcpConn.SetNoDelay(true)
	tcpConn.SetKeepAlive(true)

	local := tcpConn.LocalAddr().(*net.TCPAddr)
	bind := addrSpec{ip: local.IP, port: local.Port}
	if err := replySocks5Client(req.conn, successReply, &bind); err != nil {
		return fmt.Errorf("failed to send reply to socks5 client: %v", err)
	}

	// socks5conn := socks5conn{
	// 	TCPConn: tcpConn,
	// }

	// if cfg.UseBypass && cfg.BypassHandler != nil {
	// 	bypass := cfg.BypassHandler
	// 	if bypass.BypassAble(targetInfo.DomainName) {
	// 		bypass.HandleHttpSocks5TCP(socks5conn, targetInfo)
	// 		return nil
	// 	}
	// }

	return socks5Server.opts.Handler.HandleSocks5TCP(tcpConn, targetInfo)
}

func (socks5Server *Socks5Server) handleSocks5Bind(req *request) error {
	// TODO: Support bind
	if err := replySocks5Client(req.conn, commandNotSupported, nil); err != nil {
		return fmt.Errorf("failed to send reply: %v", err)
	}

	return fmt.Errorf("unsupport socks5 Bind command")
}

func (socks5Server *Socks5Server) handleSocks5Associate(req *request) error {
	keyUserIPCount := fmt.Sprintf("%s:%s", req.user, req.srcIP)
	socks5Server.userIPCount.incr(keyUserIPCount)
	defer socks5Server.userIPCount.decr(keyUserIPCount)

	socks5Server.userUDPCount.incr(req.user)
	defer socks5Server.userUDPCount.decr(req.user)

	udpServer, err := socks5Server.loadOrNewUserUDPServer(req.user)
	if err != nil {
		return err
	}

	logx.Debugf("Socks5Server.handleSocks5Associate udp server ip:%s, port:%d for user %s", socks5Server.opts.UDPServerIP, udpServer.port, req.user)
	ip := net.ParseIP(socks5Server.opts.UDPServerIP)
	if ip == nil {
		return fmt.Errorf("parse ip %s failed", socks5Server.opts.UDPServerIP)
	}

	bind := addrSpec{ip: ip, port: udpServer.port}
	if err := replySocks5Client(req.conn, successReply, &bind); err != nil {
		return fmt.Errorf("Socks5Server.handleSocks5Associate failed to send reply to socks5 client: %v", err)
	}

	io.Copy(io.Discard, req.conn)
	return nil
}

func (socks5Server *Socks5Server) loadOrNewUserUDPServer(userName string) (*UDPServer, error) {
	server, ok := socks5Server.userUDPServers.Load(userName)
	if ok {
		return server.(*UDPServer), nil
	}

	udpServer, err := newUDPServer(socks5Server.opts.UDPPortStart, socks5Server.opts.UDPPortEnd, socks5Server, userName)
	if err != nil {
		return nil, err
	}

	actual, loaded := socks5Server.userUDPServers.LoadOrStore(userName, udpServer)
	if loaded {
		udpServer.stop()
		return actual.(*UDPServer), nil
	}

	logx.Debugf("Socks5Server.loadOrNewUserUDPServer new udp server ip:%s, port:%d for user %s", socks5Server.opts.UDPServerIP, udpServer.port, userName)

	go udpServer.serve()
	return udpServer, nil
}
