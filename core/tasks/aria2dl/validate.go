package aria2dl

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

// AllowedSchemes defines the whitelist of protocols we accept.
// We strictly block "file://", "javascript://", "data://", etc. for security.
var allowedSchemes = map[string]struct{}{
	"http":   {},
	"https":  {},
	"ftp":    {},
	"sftp":   {},
	"ftps":   {},
	"magnet": {},
}

func ValidateURIs(uris []string) ([]string, error) {
	if len(uris) == 0 {
		return nil, fmt.Errorf("uri list is empty")
	}

	cleaned := make([]string, 0, len(uris))
	seen := make(map[string]struct{})

	for i, raw := range uris {
		trimmed := cleanString(raw)
		if trimmed == "" {
			continue
		}

		if _, exists := seen[trimmed]; exists {
			continue
		}

		parsed, err := url.Parse(trimmed)
		if err != nil {
			if isAria2Pattern(trimmed) && hasValidSchemePrefix(trimmed) {
				seen[trimmed] = struct{}{}
				cleaned = append(cleaned, trimmed)
				continue
			}
			return nil, fmt.Errorf("malformed uri at index %d: %s (%v)", i, trimmed, err)
		}

		scheme := strings.ToLower(parsed.Scheme)
		if _, allowed := allowedSchemes[scheme]; !allowed {
			return nil, fmt.Errorf("blocked insecure or unsupported scheme: '%s' in %s", scheme, trimmed)
		}

		if scheme == "magnet" {
			if err := validateMagnet(parsed); err != nil {
				return nil, fmt.Errorf("invalid magnet link: %w", err)
			}
		}

		if scheme != "magnet" && parsed.Host == "" {
			return nil, fmt.Errorf("uri missing hostname: %s", trimmed)
		}

		seen[trimmed] = struct{}{}
		cleaned = append(cleaned, trimmed)
	}

	if len(cleaned) == 0 {
		return nil, fmt.Errorf("no valid URIs provided after filtering")
	}

	return cleaned, nil
}

// cleanString removes non-printable characters and standard whitespace.
func cleanString(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || !unicode.IsPrint(r)
	})
}

// validateMagnet ensures a magnet link has the minimum required 'xt' parameter.
func validateMagnet(parsed *url.URL) error {
	var values url.Values
	if parsed.Opaque != "" {
		trimmedOpaque := strings.TrimPrefix(parsed.Opaque, "?")
		v, err := url.ParseQuery(trimmedOpaque)
		if err != nil {
			return fmt.Errorf("malformed magnet params")
		}
		values = v
	} else {
		values = parsed.Query()
	}

	if val := values.Get("xt"); val == "" {
		return fmt.Errorf("missing 'xt' (Exact Topic) hash")
	}
	return nil
}

// isAria2Pattern checks for brackets [] which indicate batch downloads.
func isAria2Pattern(s string) bool {
	return strings.Contains(s, "[") && strings.Contains(s, "]")
}

// hasValidSchemePrefix manually checks startsWith because url.Parse failed.
func hasValidSchemePrefix(s string) bool {
	lower := strings.ToLower(s)
	for scheme := range allowedSchemes {
		if strings.HasPrefix(lower, scheme+":") {
			return true
		}
	}
	return false
}
