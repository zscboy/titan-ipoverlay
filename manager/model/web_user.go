package model

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

// getMD5 calculates the MD5 hash of a string.
func getMD5(text string) string {
	hash := md5.Sum([]byte(text))
	return hex.EncodeToString(hash[:])
}

// SetWebUser stores a web user's username and MD5-hashed password in Redis.
func SetWebUser(rds *redis.Redis, username, password string) error {
	passwordMD5 := getMD5(password)
	return rds.Hset(redisKeyWebUsers, username, passwordMD5)
}

// SetWebUserMD5 stores a web user's username and direct MD5 password hash in Redis.
func SetWebUserMD5(rds *redis.Redis, username, passwordMD5 string) error {
	return rds.Hset(redisKeyWebUsers, username, passwordMD5)
}

// GetWebUserMD5 retrieves a web user's password MD5 from Redis.
func GetWebUserMD5(rds *redis.Redis, username string) (string, error) {
	md5Val, err := rds.Hget(redisKeyWebUsers, username)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", nil
		}
		return "", err
	}
	return md5Val, nil
}

// VerifyWebUser verifies a web user's credentials by MD5-hashing the password and comparing it.
func VerifyWebUser(rds *redis.Redis, username, password string) (bool, error) {
	storedMD5, err := GetWebUserMD5(rds, username)
	if err != nil {
		return false, err
	}
	fmt.Printf("username:%s,password:%s, storedPassword:%s\n", username, password, storedMD5)
	if storedMD5 == "" {
		return false, nil
	}
	fmt.Printf("storedMD5: %s, req md5: %s\n", storedMD5, getMD5(password))
	return storedMD5 == getMD5(password), nil
}

// DeleteWebUser deletes a web user from Redis.
func DeleteWebUser(rds *redis.Redis, username string) error {
	_, err := rds.Hdel(redisKeyWebUsers, username)
	return err
}

// ListWebUsers returns a map of all web users and their MD5 password hashes.
func ListWebUsers(rds *redis.Redis) (map[string]string, error) {
	return rds.Hgetall(redisKeyWebUsers)
}
