package socks5

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
)

const (
	socks5Version    = uint8(5)
	connectCommand   = uint8(1)
	bindCommand      = uint8(2)
	associateCommand = uint8(3)
	ipv4Address      = uint8(1)
	fqdnAddress      = uint8(3)
	ipv6Address      = uint8(4)

	noAuth          = uint8(0)
	userPassAuth    = uint8(2)
	userAuthVersion = uint8(1)
	userAuthSuccess = uint8(0)
	userAuthFailure = uint8(1)
	noAcceptable    = uint8(255)
)

const (
	successReply uint8 = iota
	serverFailure
	ruleFailure
	networkUnreachable
	hostUnreachable
	connectionRefused
	ttlExpired
	commandNotSupported
	addrTypeNotSupported
)

const (
	anonymousUserName = "anonymous"
)

var (
	ErrBadRequest = errors.New("Bad Request")
)

type noAuthAuthenticator struct{}

func (a noAuthAuthenticator) authenticate(writer io.Writer) error {
	_, err := writer.Write([]byte{socks5Version, noAuth})
	return err
}

func readAuthMethods(r io.Reader) ([]byte, error) {
	header := []byte{0}
	if _, err := r.Read(header); err != nil {
		return nil, err
	}

	numMethods := int(header[0])
	methods := make([]byte, numMethods)
	_, err := io.ReadAtLeast(r, methods, numMethods)
	return methods, err
}

func noAcceptableAuth(conn io.Writer) error {
	conn.Write([]byte{socks5Version, noAcceptable})
	return fmt.Errorf("not support auth method")
}

func userPassAuthFailure(conn io.Writer) error {
	if _, err := conn.Write([]byte{userAuthVersion, userAuthFailure}); err != nil {
		return err
	}
	return fmt.Errorf("user authentication failed")
}

func userPassAuthSuccess(conn io.Writer) error {
	_, err := conn.Write([]byte{userAuthVersion, userAuthSuccess})
	return err
}

// func replyAuthMethod(conn io.Writer, method uint8) error {
// 	_, err := conn.Write([]byte{socks5Version, method})
// 	return err
// }

// func authenticate(conn io.Writer, bufConn io.Reader) error {
// 	// Get the methods
// 	methods, err := readAuthMethods(bufConn)
// 	if err != nil {
// 		return fmt.Errorf("failed to get auth methods: %v", err)
// 	}

// 	// Select a usable method
// 	for _, method := range methods {
// 		found := method == noAuth
// 		if found {
// 			return noAuthAuthenticator{}.authenticate(conn)
// 		}
// 	}

// 	// No usable method found
// 	return noAcceptableAuth(conn)
// }

type addrSpec struct {
	fqdn string
	ip   net.IP
	port int
}

type request struct {
	// protocol version
	version uint8
	// requested command
	command uint8

	// AddrSpec of the desired destination
	destAddr *addrSpec

	conn      net.Conn
	bufreader *bufio.Reader
	srcIP     string
	// the user of socks5
	user string
}

func replySocks5Client(w io.Writer, resp uint8, addr *addrSpec) error {
	// Format the address
	var addrType uint8
	var addrBody []byte
	var addrPort uint16
	switch {
	case addr == nil:
		addrType = ipv4Address
		addrBody = []byte{0, 0, 0, 0}
		addrPort = 0

	case addr.fqdn != "":
		addrType = fqdnAddress
		addrBody = append([]byte{byte(len(addr.fqdn))}, addr.fqdn...)
		addrPort = uint16(addr.port)

	case addr.ip.To4() != nil:
		addrType = ipv4Address
		addrBody = []byte(addr.ip.To4())
		addrPort = uint16(addr.port)

	case addr.ip.To16() != nil:
		addrType = ipv6Address
		addrBody = []byte(addr.ip.To16())
		addrPort = uint16(addr.port)

	default:
		return fmt.Errorf("failed to format address: %v", addr)
	}

	// Format the message
	msg := make([]byte, 6+len(addrBody))
	msg[0] = socks5Version
	msg[1] = resp
	msg[2] = 0 // Reserved
	msg[3] = addrType
	copy(msg[4:], addrBody)
	msg[4+len(addrBody)] = byte(addrPort >> 8)
	msg[4+len(addrBody)+1] = byte(addrPort & 0xff)

	// Send the message
	_, err := w.Write(msg)
	return err
}

func newRequest(bufreader *bufio.Reader, conn net.Conn) (*request, error) {
	// Read the version byte
	header := []byte{0, 0, 0}

	if _, err := io.ReadAtLeast(bufreader, header, 3); err != nil {
		return nil, fmt.Errorf("localsocks5.Mgr failed to get command version: %v", err)
	}

	// Ensure we are compatible
	if header[0] != socks5Version {
		return nil, fmt.Errorf("localsocks5.Mgr unsupported command version: %v", header[0])
	}

	// Read in the destination address
	dest, err := readAddrSpec(bufreader)
	if err != nil {
		return nil, err
	}

	remoteAddr := conn.RemoteAddr().String()
	srcIP, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return nil, err
	}

	request := &request{
		version:   socks5Version,
		command:   header[1],
		destAddr:  dest,
		conn:      conn,
		bufreader: bufreader,
		srcIP:     srcIP,
		user:      anonymousUserName,
	}

	return request, nil
}

func readAddrSpec(r io.Reader) (*addrSpec, error) {
	d := &addrSpec{}

	// Get the address type
	addrType := []byte{0}
	if _, err := r.Read(addrType); err != nil {
		return nil, err
	}

	// Handle on a per type basis
	switch addrType[0] {
	case ipv4Address:
		addr := make([]byte, 4)
		if _, err := io.ReadAtLeast(r, addr, len(addr)); err != nil {
			return nil, err
		}
		d.ip = net.IP(addr)
		d.fqdn = string(d.ip.String()) // cast to domain name
	case ipv6Address:
		addr := make([]byte, 16)
		if _, err := io.ReadAtLeast(r, addr, len(addr)); err != nil {
			return nil, err
		}
		d.ip = net.IP(addr)
		d.fqdn = string(d.ip.String()) // cast to domain name
	case fqdnAddress:
		if _, err := r.Read(addrType); err != nil {
			return nil, err
		}
		addrLen := int(addrType[0])
		fqdn := make([]byte, addrLen)
		if _, err := io.ReadAtLeast(r, fqdn, addrLen); err != nil {
			return nil, err
		}
		d.fqdn = string(fqdn)

	default:
		return nil, fmt.Errorf("localsocks5.Mgr unsupport address type:%d", addrType[0])
	}

	// Read the port
	port := []byte{0, 0}
	if _, err := io.ReadAtLeast(r, port, 2); err != nil {
		return nil, err
	}
	d.port = (int(port[0]) << 8) | int(port[1])

	return d, nil
}

type datagram struct {
	rsv     []byte // 0x00 0x00
	frag    byte
	atyp    byte
	dstAddr []byte
	dstPort []byte // 2 bytes
	data    []byte
}

func (d *datagram) Bytes() []byte {
	b := make([]byte, 0)
	b = append(b, d.rsv...)
	b = append(b, d.frag)
	b = append(b, d.atyp)
	b = append(b, d.dstAddr...)
	b = append(b, d.dstPort...)
	b = append(b, d.data...)
	return b
}

func newDatagramFromBytes(bb []byte) (*datagram, error) {
	n := len(bb)
	minl := 4
	if n < minl {
		return nil, ErrBadRequest
	}
	var addr []byte
	if bb[3] == ipv4Address {
		minl += 4
		if n < minl {
			return nil, ErrBadRequest
		}
		addr = bb[minl-4 : minl]
	} else if bb[3] == ipv6Address {
		minl += 16
		if n < minl {
			return nil, ErrBadRequest
		}
		addr = bb[minl-16 : minl]
	} else if bb[3] == fqdnAddress {
		minl += 1
		if n < minl {
			return nil, ErrBadRequest
		}
		l := bb[4]
		if l == 0 {
			return nil, ErrBadRequest
		}
		minl += int(l)
		if n < minl {
			return nil, ErrBadRequest
		}
		addr = bb[minl-int(l) : minl]
		addr = append([]byte{l}, addr...)
	} else {
		return nil, ErrBadRequest
	}
	minl += 2
	if n <= minl {
		return nil, ErrBadRequest
	}
	port := bb[minl-2 : minl]
	data := bb[minl:]
	d := &datagram{
		rsv:     bb[0:2],
		frag:    bb[2],
		atyp:    bb[3],
		dstAddr: addr,
		dstPort: port,
		data:    data,
	}
	return d, nil
}

// NewDatagram return datagram packet can be writed into client, dstaddr should not have domain length
// func NewDatagram(atyp byte, dstaddr []byte, dstport []byte, data []byte) *datagram {
// 	if atyp == fqdnAddress {
// 		dstaddr = append([]byte{byte(len(dstaddr))}, dstaddr...)
// 	}
// 	return &datagram{
// 		rsv:     []byte{0x00, 0x00},
// 		frag:    0x00,
// 		atyp:    atyp,
// 		dstAddr: dstaddr,
// 		dstPort: dstport,
// 		data:    data,
// 	}
// }

func NewDatagram(addr string, data []byte) (*datagram, error) {
	atyp, dstaddr, dstport, err := parseAddress(addr)
	if err != nil {
		return nil, err
	}

	if atyp == fqdnAddress {
		dstaddr = append([]byte{byte(len(dstaddr))}, dstaddr...)
	}
	return &datagram{
		rsv:     []byte{0x00, 0x00},
		frag:    0x00,
		atyp:    atyp,
		dstAddr: dstaddr,
		dstPort: dstport,
		data:    data,
	}, nil
}

func parseAddress(address string) (a byte, addr []byte, port []byte, err error) {
	var h, p string
	h, p, err = net.SplitHostPort(address)
	if err != nil {
		return
	}
	ip := net.ParseIP(h)
	if ip4 := ip.To4(); ip4 != nil {
		a = ipv4Address
		addr = []byte(ip4)
	} else if ip6 := ip.To16(); ip6 != nil {
		a = ipv6Address
		addr = []byte(ip6)
	} else {
		a = fqdnAddress
		addr = []byte{byte(len(h))}
		addr = append(addr, []byte(h)...)
	}
	i, _ := strconv.Atoi(p)
	port = make([]byte, 2)
	binary.BigEndian.PutUint16(port, uint16(i))
	return
}

func toAddress(a byte, addr []byte, port []byte) string {
	var h, p string
	if a == ipv4Address || a == ipv6Address {
		h = net.IP(addr).String()
	}
	if a == fqdnAddress {
		if len(addr) < 1 {
			return ""
		}
		if len(addr) < int(addr[0])+1 {
			return ""
		}
		h = string(addr[1:])
	}
	p = strconv.Itoa(int(binary.BigEndian.Uint16(port)))
	return net.JoinHostPort(h, p)
}
