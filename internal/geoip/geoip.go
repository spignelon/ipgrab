// Package geoip enriches IP addresses with geolocation and network data using
// the free ip-api.com service, with an in-memory TTL cache and graceful
// degradation when lookups fail or the IP is private.
package geoip

import (
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"
)

// Result holds enrichment data for an IP. Zero values mean "unknown".
type Result struct {
	Country string
	Region  string
	City    string
	Lat     float64
	Lon     float64
	ISP     string
	Org     string
	ASN     string
}

type cacheEntry struct {
	res     Result
	expires time.Time
}

// Client looks up and caches IP metadata.
type Client struct {
	http  *http.Client
	ttl   time.Duration
	mu    sync.Mutex
	cache map[string]cacheEntry
}

// New returns a geoip client with a sane default cache TTL.
func New() *Client {
	return &Client{
		http:  &http.Client{Timeout: 4 * time.Second},
		ttl:   6 * time.Hour,
		cache: make(map[string]cacheEntry),
	}
}

// apiResponse mirrors the ip-api.com JSON fields we request.
type apiResponse struct {
	Status     string  `json:"status"`
	Country    string  `json:"country"`
	RegionName string  `json:"regionName"`
	City       string  `json:"city"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	ISP        string  `json:"isp"`
	Org        string  `json:"org"`
	AS         string  `json:"as"`
}

// Lookup enriches ip. Private, loopback, or unparseable addresses return an
// empty Result. Network/API errors also degrade to an empty Result so the
// caller can still log the raw event.
func (c *Client) Lookup(ip string) Result {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.IsLoopback() || parsed.IsPrivate() ||
		parsed.IsLinkLocalUnicast() || parsed.IsUnspecified() {
		return Result{}
	}

	c.mu.Lock()
	if e, ok := c.cache[ip]; ok && time.Now().Before(e.expires) {
		c.mu.Unlock()
		return e.res
	}
	c.mu.Unlock()

	res := c.fetch(ip)

	c.mu.Lock()
	c.cache[ip] = cacheEntry{res: res, expires: time.Now().Add(c.ttl)}
	c.mu.Unlock()
	return res
}

func (c *Client) fetch(ip string) Result {
	// Free endpoint is HTTP-only; fields param trims the response.
	url := "http://ip-api.com/json/" + ip +
		"?fields=status,country,regionName,city,lat,lon,isp,org,as"
	resp, err := c.http.Get(url)
	if err != nil {
		return Result{}
	}
	defer resp.Body.Close()

	var a apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&a); err != nil || a.Status != "success" {
		return Result{}
	}
	return Result{
		Country: a.Country,
		Region:  a.RegionName,
		City:    a.City,
		Lat:     a.Lat,
		Lon:     a.Lon,
		ISP:     a.ISP,
		Org:     a.Org,
		ASN:     a.AS,
	}
}
