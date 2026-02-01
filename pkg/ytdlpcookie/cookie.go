package ytdlpcookie

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/merisssas/bot/config"
)

const dirName = "ytdlp_cookies"

func BaseDir() string {
	return filepath.Join(filepath.Dir(config.C().DB.Path), dirName)
}

func PathForUser(userID int64) string {
	return filepath.Join(BaseDir(), fmt.Sprintf("%d.txt", userID))
}

func EnsureDir() error {
	return os.MkdirAll(BaseDir(), 0o755)
}

func SaveForUser(userID int64, data []byte) (string, error) {
	if err := EnsureDir(); err != nil {
		return "", err
	}
	path := PathForUser(userID)
	normalized := normalizeCookieData(string(data))
	if err := os.WriteFile(path, []byte(normalized), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func ExistsForUser(userID int64) (string, bool) {
	path := PathForUser(userID)
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	return path, true
}

func normalizeCookieData(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}

	lines := strings.Split(trimmed, "\n")
	hasHeader := false
	normalizedLines := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if strings.HasPrefix(line, "# Netscape HTTP Cookie File") {
				hasHeader = true
			}
			normalizedLines = append(normalizedLines, line)
			continue
		}

		if strings.Contains(line, "\t") {
			normalizedLines = append(normalizedLines, line)
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 7 {
			normalizedLines = append(normalizedLines, line)
			continue
		}

		value := strings.Join(fields[6:], " ")
		fields = append(fields[:6], value)
		normalizedLines = append(normalizedLines, strings.Join(fields, "\t"))
	}

	if !hasHeader {
		normalizedLines = append([]string{"# Netscape HTTP Cookie File"}, normalizedLines...)
	}

	return strings.Join(normalizedLines, "\n") + "\n"
}
