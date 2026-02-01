package directlinks

import (
	"fmt"
	"net/http"
	"strings"
)

func parseDirectLinkInput(raw string) (string, http.Header, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil, fmt.Errorf("empty link")
	}
	parts := strings.Split(raw, "|")
	link := strings.TrimSpace(parts[0])
	if link == "" {
		return "", nil, fmt.Errorf("missing url")
	}

	headers := make(http.Header)
	for _, part := range parts[1:] {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, ":", 2)
		if len(kv) != 2 {
			continue
		}
		key := http.CanonicalHeaderKey(strings.TrimSpace(kv[0]))
		value := strings.TrimSpace(kv[1])
		if key == "" || value == "" {
			continue
		}
		headers.Add(key, value)
	}

	if len(headers) == 0 {
		headers = nil
	}

	return link, headers, nil
}

func applyCustomHeaders(req *http.Request, headers http.Header) {
	if req == nil || len(headers) == 0 {
		return
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
}
