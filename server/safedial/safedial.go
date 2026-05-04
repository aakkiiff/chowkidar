// Package safedial provides an SSRF-safe HTTP transport that blocks outbound
// connections to private, loopback, link-local, and reserved IP ranges.
// Use it for any server-side HTTP client that fetches user-supplied URLs
// (endpoint prober, webhook poster) to prevent internal service probing.
package safedial

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

var blocked []*net.IPNet

func init() {
	for _, cidr := range []string{
		"127.0.0.0/8",    // loopback
		"10.0.0.0/8",     // RFC1918
		"172.16.0.0/12",  // RFC1918
		"192.168.0.0/16", // RFC1918
		"169.254.0.0/16", // link-local + AWS/GCP metadata (169.254.169.254)
		"100.64.0.0/10",  // carrier-grade NAT
		"0.0.0.0/8",      // this network
		"240.0.0.0/4",    // reserved
		"198.18.0.0/15",  // benchmarking
		"::1/128",        // IPv6 loopback
		"fc00::/7",       // IPv6 unique local
		"fe80::/10",      // IPv6 link-local
	} {
		_, n, _ := net.ParseCIDR(cidr)
		if n != nil {
			blocked = append(blocked, n)
		}
	}
}

func isBlocked(ip net.IP) bool {
	for _, n := range blocked {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// Transport returns an *http.Transport whose DialContext resolves the target
// hostname and rejects any IP that falls within a private/reserved range.
func Transport() *http.Transport {
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupHost(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, ipStr := range ips {
				ip := net.ParseIP(ipStr)
				if ip == nil || isBlocked(ip) {
					return nil, fmt.Errorf("host %s resolves to blocked address %s", host, ipStr)
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       60 * time.Second,
	}
}
