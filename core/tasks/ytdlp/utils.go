package ytdlp

import (
	"net/url"
	"strconv"
	"strings"
)

func parseByteSize(input string) (int64, bool) {
	trimmed := strings.TrimSpace(strings.ToUpper(input))
	if trimmed == "" {
		return 0, false
	}

	valuePart := trimmed
	unitPart := ""
	for i, r := range trimmed {
		if r < '0' || r > '9' {
			if r != '.' {
				valuePart = strings.TrimSpace(trimmed[:i])
				unitPart = strings.TrimSpace(trimmed[i:])
				break
			}
		}
	}

	value, err := strconv.ParseFloat(valuePart, 64)
	if err != nil {
		return 0, false
	}

	multiplier := float64(1)
	switch unitPart {
	case "", "B":
		multiplier = 1
	case "K", "KB":
		multiplier = 1024
	case "M", "MB":
		multiplier = 1024 * 1024
	case "G", "GB":
		multiplier = 1024 * 1024 * 1024
	case "T", "TB":
		multiplier = 1024 * 1024 * 1024 * 1024
	default:
		return 0, false
	}

	return int64(value * multiplier), true
}

func formatByteSize(value int64) string {
	if value <= 0 {
		return ""
	}
	const unit = 1024
	if value < unit {
		return strconv.FormatInt(value, 10) + "B"
	}
	div, exp := int64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return strconv.FormatFloat(float64(value)/float64(div), 'f', 0, 64) + string("KMGTPE"[exp]) + "B"
}

func hostFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}
