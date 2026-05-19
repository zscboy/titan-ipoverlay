package model

import (
	"testing"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

func TestWebUser(t *testing.T) {
	conf := redis.RedisConf{Host: "127.0.0.1:6379", Type: "node"}
	rd := redis.MustNewRedis(conf)

	username := "test_web_user"
	password := "secure_pass_123"

	// 1. Clean up first
	_ = DeleteWebUser(rd, username)
	_ = DeleteWebUser(rd, "admin")

	// 2. Test MD5 hashing
	expectedMD5 := getMD5(password)
	if expectedMD5 == "" {
		t.Errorf("MD5 hash should not be empty")
	}

	// 3. Test SetWebUser
	err := SetWebUser(rd, username, password)
	if err != nil {
		t.Fatalf("failed to set web user: %v", err)
	}

	// 4. Test GetWebUserMD5
	storedMD5, err := GetWebUserMD5(rd, username)
	if err != nil {
		t.Fatalf("failed to get web user MD5: %v", err)
	}
	if storedMD5 != expectedMD5 {
		t.Errorf("expected MD5 %s, got %s", expectedMD5, storedMD5)
	}

	// 5. Test VerifyWebUser
	ok, err := VerifyWebUser(rd, username, password)
	if err != nil {
		t.Fatalf("failed to verify web user: %v", err)
	}
	if !ok {
		t.Errorf("expected credentials to be valid")
	}

	ok, err = VerifyWebUser(rd, username, "wrong_password")
	if err != nil {
		t.Fatalf("failed to verify web user with wrong pass: %v", err)
	}
	if ok {
		t.Errorf("expected credentials to be invalid for wrong password")
	}

	adminMD5, err := GetWebUserMD5(rd, "admin")
	if err != nil {
		t.Fatalf("failed to get admin user MD5: %v", err)
	}
	expectedAdminMD5 := getMD5("titan123")
	if adminMD5 != expectedAdminMD5 {
		t.Errorf("expected admin MD5 %s, got %s", expectedAdminMD5, adminMD5)
	}

	// 7. Test ListWebUsers
	users, err := ListWebUsers(rd)
	if err != nil {
		t.Fatalf("failed to list web users: %v", err)
	}
	if users[username] != expectedMD5 {
		t.Errorf("expected user %s to be listed with correct MD5", username)
	}
	if users["admin"] != expectedAdminMD5 {
		t.Errorf("expected admin to be listed with correct MD5")
	}

	// 8. Test DeleteWebUser
	err = DeleteWebUser(rd, username)
	if err != nil {
		t.Fatalf("failed to delete web user: %v", err)
	}

	storedMD5, err = GetWebUserMD5(rd, username)
	if err != nil {
		t.Fatalf("failed to get deleted web user MD5: %v", err)
	}
	if storedMD5 != "" {
		t.Errorf("expected deleted user to return empty MD5, got %s", storedMD5)
	}

	// Clean up admin
	_ = DeleteWebUser(rd, "admin")
}
