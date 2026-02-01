package aria2dl

import (
	"fmt"
	"net/url"
	"strings"
)

func ValidateURIs(uris []string) ([]string, error) {
	cleaned := make([]string, 0, len(uris))
	for _, raw := range uris {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}

		parsed, err := url.Parse(trimmed)
		if err != nil || parsed.Scheme == "" {
			return nil, fmt.Errorf("invalid uri: %s", trimmed)
		}

		cleaned = append(cleaned, trimmed)
	}

	if len(cleaned) == 0 {
		return nil, fmt.Errorf("no valid URIs provided")
	}

	return cleaned, nil
}
