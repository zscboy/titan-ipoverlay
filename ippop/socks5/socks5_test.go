package socks5

import "testing"

func TestPaserUserName(t *testing.T) {
	username := "titan-zone-static-region-us-session-1312a838o-sessTime-5"
	user, err := paserUsername(username)
	if err != nil {
		t.Logf("err:%v", err)
		return
	}
	t.Logf("user:%#v", *user)
}
