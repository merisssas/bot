package netutil

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

var AllowedSchemesDirect = map[string]struct{}{
	"http":  {},
	"https": {},
}

var AllowedSchemesAria2 = map[string]struct{}{
	"http":   {},
	"https":  {},
	"ftp":    {},
	"ftps":   {},
	"magnet": {},
}

// ValidatePublicURL parses a URL, validates allowed schemes, and blocks private or local addresses.
func ValidatePublicURL(ctx context.Context, raw string, allowedSchemes map[string]struct{}, allowEmptyHost bool) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("empty URL")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if _, ok := allowedSchemes[scheme]; !ok {
		return "", fmt.Errorf("unsupported scheme: %s", scheme)
	}
	if parsed.Host == "" {
		if !allowEmptyHost || scheme != "magnet" {
			return "", fmt.Errorf("missing host")
		}
		return parsed.String(), nil
	}
	host := parsed.Hostname()
	if host == "" {
		return "", fmt.Errorf("missing host")
	}
	if err := validatePublicHost(ctx, host); err != nil {
		return "", err
	}
	return parsed.String(), nil
}

func validatePublicHost(ctx context.Context, host string) error {
	lowerHost := strings.ToLower(host)
	if lowerHost == "localhost" || strings.HasSuffix(lowerHost, ".localhost") {
		return fmt.Errorf("local host is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return fmt.Errorf("private IP is not allowed")
		}
		return nil
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(lookupCtx, "ip", host)
	if err != nil {
		return fmt.Errorf("failed to resolve host: %w", err)
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("private IP is not allowed")
		}
	}
	return nil
}

func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	return false
}
