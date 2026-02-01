package directlinks

import (
	"net/http"
	"testing"
)

func TestParseDirectLinkInput(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectURL      string
		expectHeaders  http.Header
		expectHasError bool
	}{
		{
			name:      "plain url",
			input:     "https://example.com/file.zip",
			expectURL: "https://example.com/file.zip",
		},
		{
			name:      "headers with cookie and referer",
			input:     "http://example.com/file.zip|Referer:google.com|Cookie:auth=123",
			expectURL: "http://example.com/file.zip",
			expectHeaders: http.Header{
				"Referer": {"google.com"},
				"Cookie":  {"auth=123"},
			},
		},
		{
			name:      "multiple cookie headers",
			input:     "http://example.com/file.zip|Cookie:auth=123|Cookie:auth=456",
			expectURL: "http://example.com/file.zip",
			expectHeaders: http.Header{
				"Cookie": {"auth=123", "auth=456"},
			},
		},
		{
			name:      "invalid header ignored",
			input:     "http://example.com/file.zip|InvalidHeader",
			expectURL: "http://example.com/file.zip",
		},
		{
			name:           "empty input",
			input:          "",
			expectHasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, headers, err := parseDirectLinkInput(tt.input)
			if tt.expectHasError {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if url != tt.expectURL {
				t.Fatalf("expected url %q, got %q", tt.expectURL, url)
			}
			if len(tt.expectHeaders) == 0 {
				if len(headers) != 0 {
					t.Fatalf("expected empty headers, got %v", headers)
				}
				return
			}
			for key, expectedValues := range tt.expectHeaders {
				values := headers.Values(key)
				if len(values) != len(expectedValues) {
					t.Fatalf("expected %d values for %s, got %d", len(expectedValues), key, len(values))
				}
				for i, value := range expectedValues {
					if values[i] != value {
						t.Fatalf("expected header %s value %q, got %q", key, value, values[i])
					}
				}
			}
		})
	}
}
