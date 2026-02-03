package aria2dl

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/log"

	"github.com/merisssas/Bot/config"
	"github.com/merisssas/Bot/pkg/aria2"
)

// --- Constants & Enums ---

type OverwritePolicy string

const (
	PolicyRename    OverwritePolicy = "rename"
	PolicyOverwrite OverwritePolicy = "overwrite"
	PolicySkip      OverwritePolicy = "skip"

	defaultMaxRetries          = 5
	defaultRetryBaseDelay      = 500 * time.Millisecond
	defaultRetryMaxDelay       = 10 * time.Second
	defaultSplit               = 8
	defaultMaxConnPerServer    = 4
	defaultMinSplitSize        = "1MB"
	defaultBurstDuration       = 10 * time.Second
	defaultUserAgent           = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) UltimateDownloader/2.0"
	defaultEnableResume        = true
	defaultEnableIntegrityScan = true

	// Aria2 hard limits to prevent runtime errors.
	MaxAria2Split       = 16
	MaxAria2Connections = 16

	// Network resilience defaults.
	DefaultConnectTimeout = "60"
	DefaultGlobalTimeout  = "600"
	DefaultLowestSpeed    = "10K"
)

var supportedChecksums = map[string]bool{
	"md5":     true,
	"sha-1":   true,
	"sha-256": true,
	"sha-512": true,
}

// DefaultTaskConfig constructs a sanitized, ready-to-use configuration.
// It acts as an anticorruption layer between raw user config and the downloader.
func DefaultTaskConfig() TaskConfig {
	raw := config.C()
	aria2Raw := raw.Aria2

	proxyURL := strings.TrimSpace(aria2Raw.Proxy)
	if proxyURL == "" {
		proxyURL = strings.TrimSpace(raw.Proxy)
	}

	policy := OverwritePolicy(strings.ToLower(strings.TrimSpace(aria2Raw.OverwritePolicy)))
	switch policy {
	case PolicyRename, PolicyOverwrite, PolicySkip:
		// valid
	default:
		policy = PolicyRename
	}

	ua := strings.TrimSpace(aria2Raw.UserAgent)
	if ua == "" {
		ua = defaultUserAgent
	}

	algo := strings.ToLower(strings.TrimSpace(aria2Raw.ChecksumAlgorithm))
	hash := strings.TrimSpace(aria2Raw.ExpectedChecksum)
	enableIntegrity := defaultEnableIntegrityScan

	if algo != "" && hash != "" {
		if !supportedChecksums[algo] {
			log.Warnf("Unsupported checksum algorithm '%s' ignored. Supported: md5, sha-1, sha-256, sha-512", algo)
			algo = ""
			hash = ""
			enableIntegrity = false
		} else {
			enableIntegrity = true
		}
	} else {
		algo = ""
		hash = ""
		enableIntegrity = false
	}

	return TaskConfig{
		MaxRetries:       clampInt(aria2Raw.MaxRetries, 0, 100, defaultMaxRetries),
		RetryBaseDelay:   clampDuration(aria2Raw.RetryBaseDelay, 100*time.Millisecond, defaultRetryBaseDelay),
		RetryMaxDelay:    clampDuration(aria2Raw.RetryMaxDelay, 1*time.Second, defaultRetryMaxDelay),
		Priority:         aria2Raw.DefaultPriority,
		VerifyHash:       algo != "" && hash != "",
		HashType:         algo,
		ExpectedHash:     hash,
		CustomHeaders:    aria2Raw.Headers,
		ProxyURL:         proxyURL,
		LimitRate:        strings.TrimSpace(aria2Raw.LimitRate),
		BurstRate:        strings.TrimSpace(aria2Raw.BurstRate),
		BurstDuration:    clampDuration(aria2Raw.BurstDuration, 1*time.Second, defaultBurstDuration),
		UserAgent:        ua,
		EnableResume:     resolveBool(aria2Raw.EnableResume, defaultEnableResume),
		Split:            clampInt(aria2Raw.Split, 1, MaxAria2Split, defaultSplit),
		MaxConnPerServer: clampInt(aria2Raw.MaxConnPerServer, 1, MaxAria2Connections, defaultMaxConnPerServer),
		MinSplitSize:     resolveString(aria2Raw.MinSplitSize, defaultMinSplitSize),
		OverwritePolicy:  policy,
		DryRun:           aria2Raw.DryRun,

		EnableIntegrityScan: enableIntegrity,
	}
}

// BuildAria2Options converts internal config to raw Aria2 RPC options.
// This function guarantees that the output map is 100% compliant with Aria2 RPC spec.
func BuildAria2Options(cfg TaskConfig) (aria2.Options, error) {
	opts := make(aria2.Options)

	opts["user-agent"] = cfg.UserAgent
	opts["connect-timeout"] = DefaultConnectTimeout
	opts["timeout"] = DefaultGlobalTimeout
	opts["lowest-speed-limit"] = DefaultLowestSpeed

	if cfg.ProxyURL != "" {
		opts["all-proxy"] = cfg.ProxyURL
	}

	opts["split"] = cfg.Split
	opts["max-connection-per-server"] = cfg.MaxConnPerServer
	opts["min-split-size"] = cfg.MinSplitSize
	opts["max-tries"] = cfg.MaxRetries
	opts["retry-wait"] = int(cfg.RetryBaseDelay.Seconds())

	if cfg.LimitRate != "" {
		opts["max-download-limit"] = cfg.LimitRate
	}

	if len(cfg.CustomHeaders) > 0 {
		headers := make([]string, 0, len(cfg.CustomHeaders))
		for key, value := range cfg.CustomHeaders {
			safeKey := http.CanonicalHeaderKey(strings.TrimSpace(key))
			safeValue := strings.TrimSpace(value)
			if safeKey == "" || safeValue == "" {
				continue
			}
			headers = append(headers, fmt.Sprintf("%s: %s", safeKey, safeValue))
		}
		if len(headers) > 0 {
			opts["header"] = headers
		}
	}

	if cfg.EnableResume {
		opts["continue"] = "true"
	}

	if cfg.VerifyHash {
		opts["checksum"] = fmt.Sprintf("%s=%s", cfg.HashType, cfg.ExpectedHash)
		opts["check-integrity"] = "true"
	} else if cfg.EnableIntegrityScan {
		opts["check-integrity"] = "true"
	}

	applyPolicyRules(opts, cfg.OverwritePolicy)

	return opts, nil
}

// applyPolicyRules translates high-level policy to specific boolean flags.
func applyPolicyRules(opts aria2.Options, policy OverwritePolicy) {
	switch policy {
	case PolicyOverwrite:
		opts["allow-overwrite"] = "true"
		opts["auto-file-renaming"] = "false"
	case PolicySkip:
		opts["allow-overwrite"] = "false"
		opts["auto-file-renaming"] = "false"
	default:
		opts["allow-overwrite"] = "false"
		opts["auto-file-renaming"] = "true"
	}
}

// ApplyQueuePriority dynamically adjusts task position in the queue.
func ApplyQueuePriority(ctx context.Context, client *aria2.Client, gid string, priority int) error {
	if client == nil || gid == "" || priority == 0 {
		return nil
	}

	method := "POS_END"
	position := 0

	if priority > 0 {
		method = "POS_SET"
		position = 0
	}

	_, err := client.ChangePosition(ctx, gid, position, method)
	if err != nil {
		return fmt.Errorf("failed to change priority for GID %s: %w", gid, err)
	}
	return nil
}

// --- Helper Functions (Clampers) ---

func clampInt(val, min, max, def int) int {
	if val <= 0 {
		return def
	}
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

func clampDuration(val, min, def time.Duration) time.Duration {
	if val <= 0 {
		return def
	}
	if val < min {
		return min
	}
	return val
}

func resolveBool(ptr *bool, def bool) bool {
	if ptr == nil {
		return def
	}
	return *ptr
}

func resolveString(val, def string) string {
	if strings.TrimSpace(val) == "" {
		return def
	}
	return val
}
