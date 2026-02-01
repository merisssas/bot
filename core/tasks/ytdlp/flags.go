package ytdlp

import (
	"fmt"
	"strconv"
	"strings"
)

func applyControlFlags(cfg TaskConfig, flags []string) (TaskConfig, []string, error) {
	cleaned := make([]string, 0, len(flags))

	for i := 0; i < len(flags); i++ {
		flag := strings.TrimSpace(flags[i])
		if flag == "" {
			continue
		}

		if !isControlFlag(flag) {
			cleaned = append(cleaned, flag)
			continue
		}

		key, value, usedNext := parseFlagValue(flag, flags, i)
		if usedNext {
			i++
		}

		switch key {
		case "sa-dry-run", "saveany-dry-run":
			if value == "" || strings.EqualFold(value, "true") {
				cfg.DryRun = true
			} else if strings.EqualFold(value, "false") {
				cfg.DryRun = false
			} else {
				return cfg, nil, fmt.Errorf("invalid dry-run value: %s", value)
			}
		case "sa-overwrite", "saveany-overwrite":
			cfg.OverwritePolicy = parseOverwritePolicy(value)
		case "sa-checksum", "saveany-checksum":
			algo, expected := splitChecksum(value)
			cfg.ChecksumAlgorithm = algo
			cfg.ExpectedChecksum = expected
		case "sa-checksum-file", "saveany-checksum-file":
			if value == "" || strings.EqualFold(value, "true") {
				cfg.WriteChecksumFile = true
			} else if strings.EqualFold(value, "false") {
				cfg.WriteChecksumFile = false
			} else {
				return cfg, nil, fmt.Errorf("invalid checksum-file value: %s", value)
			}
		case "sa-proxy", "saveany-proxy":
			cfg.Proxy = value
		case "sa-limit", "saveany-limit":
			cfg.LimitRate = value
		case "sa-burst", "saveany-burst":
			cfg.ThrottledRate = value
		case "sa-conn", "saveany-conn":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return cfg, nil, fmt.Errorf("invalid download concurrency: %w", err)
			}
			cfg.DownloadConcurrency = parsed
		case "sa-fragments", "saveany-fragments":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return cfg, nil, fmt.Errorf("invalid fragment concurrency: %w", err)
			}
			cfg.FragmentConcurrency = parsed
		case "sa-retries", "saveany-retries":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return cfg, nil, fmt.Errorf("invalid retry count: %w", err)
			}
			cfg.MaxRetries = parsed
		case "sa-priority", "saveany-priority":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return cfg, nil, fmt.Errorf("invalid priority: %w", err)
			}
			cfg.Priority = parsed
		default:
			return cfg, nil, fmt.Errorf("unknown control flag: %s", key)
		}
	}

	return cfg, cleaned, nil
}

func isControlFlag(flag string) bool {
	return strings.HasPrefix(flag, "--sa-") || strings.HasPrefix(flag, "--saveany-")
}

func parseFlagValue(flag string, flags []string, idx int) (string, string, bool) {
	flag = strings.TrimPrefix(flag, "--")
	parts := strings.SplitN(flag, "=", 2)
	if len(parts) == 2 {
		return parts[0], strings.TrimSpace(parts[1]), false
	}

	next := ""
	if idx+1 < len(flags) {
		nextCandidate := strings.TrimSpace(flags[idx+1])
		if nextCandidate != "" && !strings.HasPrefix(nextCandidate, "-") {
			next = nextCandidate
			return parts[0], next, true
		}
	}

	return parts[0], "", false
}

func splitChecksum(value string) (string, string) {
	if value == "" {
		return "", ""
	}
	parts := strings.SplitN(value, ":", 2)
	algo := strings.TrimSpace(parts[0])
	if len(parts) == 1 {
		return algo, ""
	}
	return algo, strings.TrimSpace(parts[1])
}
