package ytdlpcookie

import (
	"strings"
	"testing"
)

func TestNormalizeCookieData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:  "adds header and normalizes spaces to tabs",
			input: ".youtube.com TRUE / TRUE 1804032538 __Secure-3PAPISID value\n",
			expected: strings.Join([]string{
				"# Netscape HTTP Cookie File",
				".youtube.com\tTRUE\t/\tTRUE\t1804032538\t__Secure-3PAPISID\tvalue",
				"",
			}, "\n"),
		},
		{
			name: "preserves header and tabs",
			input: strings.Join([]string{
				"# Netscape HTTP Cookie File",
				".youtube.com\tTRUE\t/\tTRUE\t1804032538\t__Secure-3PAPISID\tvalue",
				"",
			}, "\n"),
			expected: strings.Join([]string{
				"# Netscape HTTP Cookie File",
				".youtube.com\tTRUE\t/\tTRUE\t1804032538\t__Secure-3PAPISID\tvalue",
				"",
			}, "\n"),
		},
		{
			name: "keeps lines with fewer fields unchanged",
			input: strings.Join([]string{
				"# Netscape HTTP Cookie File",
				"invalid line",
			}, "\n"),
			expected: strings.Join([]string{
				"# Netscape HTTP Cookie File",
				"invalid line",
				"",
			}, "\n"),
		},
		{
			name:  "empty input returns empty output",
			input: " \n\t",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := normalizeCookieData(tt.input)
			if result != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}
