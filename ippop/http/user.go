package http

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type User struct {
	username string
	zone     string
	region   string
	session  string
	// duration
	sessTime time.Duration
}

// username="ceshi1_11-zone-static-region-us-session-1312a838o-sessTime-5"
func paserUsername(username string) (*User, error) {
	parts := strings.Split(username, "-")
	if len(parts) < 1 {
		return nil, errors.New("invalid username string")
	}

	keys := map[string]bool{
		"zone":     true,
		"region":   true,
		"session":  true,
		"sessTime": true,
	}

	user := &User{
		username: parts[0],
	}

	i := 1
	for i < len(parts) {
		key := parts[i]
		if !keys[key] {
			i++
			continue
		}

		j := i + 1
		for j < len(parts) && !keys[parts[j]] {
			j++
		}

		value := strings.Join(parts[i+1:j], "-")

		switch key {
		case "zone":
			user.zone = value
		case "region":
			user.region = value
		case "session":
			user.session = value
		case "sessTime":
			if n, err := strconv.Atoi(value); err == nil {
				user.sessTime = time.Duration(n) * time.Minute
			}
		}

		i = j
	}

	if user.session != "" {
		if err := validateSession(user.session); err != nil {
			return nil, err
		}
	}

	return user, nil
}

var sessionRegex = regexp.MustCompile(`^[0-9A-Za-z]{1,16}$`)

func validateSession(session string) error {
	if !sessionRegex.MatchString(session) {
		return fmt.Errorf("invalid session: %q (must be 1~9 chars of 0-9, a-z, A-Z)", session)
	}
	return nil
}
