package socks5

import (
	"net"
	"testing"
)

func TestPaserUserName(t *testing.T) {
	username := "test1"
	user, err := paserUsername(username)
	if err != nil {
		t.Logf("err:%v", err)
		return
	}
	t.Logf("user:%#v", *user)
}

func TestIPBlacklist(t *testing.T) {
	blacklist := newIPBlacklist()
	blacklist.Add([]net.IP{net.ParseIP("192.0.2.1"), net.ParseIP("2001:db8::1")})
	if !blacklist.Contains(net.ParseIP("192.0.2.1")) || !blacklist.Contains(net.ParseIP("2001:db8::1")) {
		t.Fatal("added IP was not found in blacklist")
	}
	blacklist.Remove([]net.IP{net.ParseIP("192.0.2.1")})
	if blacklist.Contains(net.ParseIP("192.0.2.1")) {
		t.Fatal("removed IP remains in blacklist")
	}
}
