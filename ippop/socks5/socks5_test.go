package socks5

import "testing"

func TestPaserUserName(t *testing.T) {
	username := "test1"
	user, err := paserUsername(username)
	if err != nil {
		t.Logf("err:%v", err)
		return
	}
	t.Logf("user:%#v", *user)
}
