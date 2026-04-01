package security

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

var blockedHostnames = map[string]struct{}{
	"localhost":                {},
	"metadata.google.internal": {},
}

// ValidateWebFetchURL returns an error if u is not a safe http(s) URL for agent web_fetch (SSRF mitigation).
func ValidateWebFetchURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("empty URL")
	}
	pu, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse URL: %w", err)
	}
	if pu.Scheme != "http" && pu.Scheme != "https" {
		return fmt.Errorf("only http and https are allowed")
	}
	if pu.Opaque != "" || pu.User != nil {
		return fmt.Errorf("URL must not contain userinfo or opaque form")
	}
	host := pu.Hostname()
	if host == "" {
		return fmt.Errorf("missing host")
	}
	hl := strings.ToLower(strings.TrimSuffix(host, "."))
	if _, bad := blockedHostnames[hl]; bad {
		return fmt.Errorf("hostname %q is not allowed", host)
	}
	port := pu.Port()
	if port == "" {
		// default ports only
	} else if port != "80" && port != "443" {
		return fmt.Errorf("port %s is not allowed (only 80 and 443)", port)
	}

	if ip, err := netip.ParseAddr(host); err == nil {
		if !isPublicIP(ip) {
			return fmt.Errorf("IP %s is not a public endpoint", host)
		}
		return nil
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve host: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("no addresses for host")
	}
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			return fmt.Errorf("invalid resolved address")
		}
		if !isPublicIP(addr) {
			return fmt.Errorf("resolved address %s is not public", addr)
		}
	}
	return nil
}

func isPublicIP(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	if ip.IsPrivate() || ip.IsUnspecified() {
		return false
	}
	// Unique local IPv6 (fc00::/7)
	if ip.Is6() {
		b := ip.As16()
		if b[0] == 0xfc || b[0] == 0xfd {
			return false
		}
	}
	// IPv4-mapped private / loopback inside IPv6
	if ip.Is4In6() {
		v4 := ip.Unmap()
		return isPublicIP(v4)
	}
	return true
}
