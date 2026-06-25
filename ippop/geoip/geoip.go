package geoip

import (
	"net"
	"strings"
	"sync"

	"github.com/oschwald/geoip2-golang"
	"github.com/zeromicro/go-zero/core/logx"
)

var (
	mu     sync.RWMutex
	reader *geoip2.Reader
)

// Init opens the GeoIP database. If dbPath is empty, GeoIP lookup will be disabled.
func Init(dbPath string) error {
	mu.Lock()
	defer mu.Unlock()

	// Close existing reader if any
	if reader != nil {
		reader.Close()
		reader = nil
	}

	if dbPath == "" {
		logx.Info("GeoIP: No database path configured. GeoIP country lookup is disabled.")
		return nil
	}

	r, err := geoip2.Open(dbPath)
	if err != nil {
		logx.Errorf("GeoIP: Failed to open database at %q: %v. GeoIP lookup is disabled.", dbPath, err)
		// We return nil to allow the application to boot normally even if the optional DB file is missing
		return nil
	}

	reader = r
	logx.Infof("GeoIP: Database successfully loaded from %q", dbPath)
	return nil
}

// LookupCountry returns the lowercase ISO 3166-1 alpha-2 country code for the given IP address.
// Returns an empty string if lookup is disabled, the IP is invalid, or the country code cannot be determined.
func LookupCountry(ipStr string) string {
	mu.RLock()
	r := reader
	mu.RUnlock()

	if r == nil {
		return ""
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return ""
	}

	record, err := r.Country(ip)
	if err != nil {
		logx.Debugf("GeoIP: Lookup failed for IP %s: %v", ipStr, err)
		return ""
	}

	return strings.ToLower(record.Country.IsoCode)
}

// Close closes the GeoIP database reader if it is open.
func Close() {
	mu.Lock()
	defer mu.Unlock()

	if reader != nil {
		reader.Close()
		reader = nil
	}
}
