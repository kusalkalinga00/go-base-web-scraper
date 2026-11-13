// Package urlguard validates user supplied URLs before they are fetched.
//
// Scraping arbitrary URLs from the server means every submitted URL is
// potentially an SSRF vector: cloud metadata endpoints, internal admin
// panels, or local services. Validate rejects non-HTTP schemes and hosts
// that resolve to loopback, private, link-local, or otherwise reserved
// addresses.
package urlguard

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

const resolveTimeout = 5 * time.Second

// Validate parses raw and verifies that it is an absolute http(s) URL whose
// host resolves only to public addresses.
func Validate(ctx context.Context, raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported url scheme %q: only http and https are allowed", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("url is missing a host")
	}

	host := strings.ToLower(u.Hostname())
	if host == "" {
		return nil, fmt.Errorf("url is missing a host")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return nil, fmt.Errorf("url host %q is not allowed", host)
	}

	// Literal IPs do not need a DNS lookup.
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		if err := checkIP(ip); err != nil {
			return nil, err
		}
		return u, nil
	}

	lookupCtx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()

	ips, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve url host %q: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("url host %q did not resolve to any address", host)
	}
	for _, addr := range ips {
		if err := checkIP(addr.IP); err != nil {
			return nil, err
		}
	}
	return u, nil
}

func checkIP(ip net.IP) error {
	if isDisallowedIP(ip) {
		return fmt.Errorf("url resolves to a non-public address (%s)", ip)
	}
	return nil
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func isDisallowedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	return ip.IsUnspecified() ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() ||
		cgnat.Contains(ip)
}
