package aria2dl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/merisssas/Bot/config"
	"github.com/merisssas/Bot/pkg/aria2"
)

const (
	defaultMaxRetries          = 5
	defaultRetryBaseDelay      = 500 * time.Millisecond
	defaultRetryMaxDelay       = 10 * time.Second
	defaultSplit               = 8
	defaultMaxConnPerServer    = 4
	defaultMinSplitSize        = "1MB"
	defaultOverwritePolicy     = "rename"
	defaultBurstDuration       = 10 * time.Second
	defaultUserAgent           = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) UltimateDownloader/2.0"
	defaultEnableResume        = true
	defaultEnableIntegrityScan = true
)

var supportedChecksumAlgorithms = map[string]struct{}{
	"md5":     {},
	"sha-1":   {},
	"sha-256": {},
	"sha-512": {},
}

// DefaultTaskConfig builds the default aria2 task configuration from viper config values.
func DefaultTaskConfig() TaskConfig {
	cfg := config.C()
	aria2Cfg := cfg.Aria2

	maxRetries := aria2Cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}

	retryBaseDelay := aria2Cfg.RetryBaseDelay
	if retryBaseDelay <= 0 {
		retryBaseDelay = defaultRetryBaseDelay
	}

	retryMaxDelay := aria2Cfg.RetryMaxDelay
	if retryMaxDelay <= 0 {
		retryMaxDelay = defaultRetryMaxDelay
	}

	split := aria2Cfg.Split
	if split <= 0 {
		split = defaultSplit
	}

	maxConnPerServer := aria2Cfg.MaxConnPerServer
	if maxConnPerServer <= 0 {
		maxConnPerServer = defaultMaxConnPerServer
	}

	minSplitSize := strings.TrimSpace(aria2Cfg.MinSplitSize)
	if minSplitSize == "" {
		minSplitSize = defaultMinSplitSize
	}

	overwritePolicy := strings.ToLower(strings.TrimSpace(aria2Cfg.OverwritePolicy))
	if overwritePolicy == "" {
		overwritePolicy = defaultOverwritePolicy
	}

	userAgent := strings.TrimSpace(aria2Cfg.UserAgent)
	if userAgent == "" {
		userAgent = defaultUserAgent
	}

	burstDuration := aria2Cfg.BurstDuration
	if burstDuration <= 0 {
		burstDuration = defaultBurstDuration
	}

	enableResume := defaultEnableResume
	if aria2Cfg.EnableResume != nil {
		enableResume = *aria2Cfg.EnableResume
	}

	enableIntegrityScan := defaultEnableIntegrityScan
	if aria2Cfg.ChecksumAlgorithm == "" || aria2Cfg.ExpectedChecksum == "" {
		enableIntegrityScan = false
	}

	proxy := strings.TrimSpace(aria2Cfg.Proxy)
	if proxy == "" {
		proxy = strings.TrimSpace(cfg.Proxy)
	}

	return TaskConfig{
		MaxRetries:          maxRetries,
		RetryBaseDelay:      retryBaseDelay,
		RetryMaxDelay:       retryMaxDelay,
		Priority:            aria2Cfg.DefaultPriority,
		VerifyHash:          aria2Cfg.ChecksumAlgorithm != "" && aria2Cfg.ExpectedChecksum != "",
		HashType:            strings.ToLower(strings.TrimSpace(aria2Cfg.ChecksumAlgorithm)),
		ExpectedHash:        strings.TrimSpace(aria2Cfg.ExpectedChecksum),
		CustomHeaders:       aria2Cfg.Headers,
		ProxyURL:            proxy,
		LimitRate:           strings.TrimSpace(aria2Cfg.LimitRate),
		BurstRate:           strings.TrimSpace(aria2Cfg.BurstRate),
		BurstDuration:       burstDuration,
		UserAgent:           userAgent,
		EnableResume:        enableResume,
		Split:               split,
		MaxConnPerServer:    maxConnPerServer,
		MinSplitSize:        minSplitSize,
		OverwritePolicy:     overwritePolicy,
		DryRun:              aria2Cfg.DryRun,
		ChecksumAlgorithm:   strings.ToLower(strings.TrimSpace(aria2Cfg.ChecksumAlgorithm)),
		ExpectedChecksum:    strings.TrimSpace(aria2Cfg.ExpectedChecksum),
		RequireChecksum:     false,
		EnableIntegrityScan: enableIntegrityScan,
	}
}

func BuildAria2Options(taskConfig TaskConfig) (aria2.Options, error) {
	options := aria2.Options{}

	if taskConfig.EnableResume {
		options["continue"] = true
	}

	if taskConfig.Split > 0 {
		options["split"] = taskConfig.Split
	}

	if taskConfig.MaxConnPerServer > 0 {
		options["max-connection-per-server"] = taskConfig.MaxConnPerServer
	}

	if taskConfig.MinSplitSize != "" {
		options["min-split-size"] = taskConfig.MinSplitSize
	}

	if taskConfig.MaxRetries > 0 {
		options["max-tries"] = taskConfig.MaxRetries
	}

	if taskConfig.RetryBaseDelay > 0 {
		options["retry-wait"] = int(taskConfig.RetryBaseDelay.Seconds())
	}

	if taskConfig.LimitRate != "" {
		options["max-download-limit"] = taskConfig.LimitRate
	}

	if taskConfig.ProxyURL != "" {
		options["all-proxy"] = taskConfig.ProxyURL
	}

	if taskConfig.UserAgent != "" {
		options["user-agent"] = taskConfig.UserAgent
	}

	if len(taskConfig.CustomHeaders) > 0 {
		headers := make([]string, 0, len(taskConfig.CustomHeaders))
		for key, value := range taskConfig.CustomHeaders {
			headers = append(headers, fmt.Sprintf("%s: %s", key, value))
		}
		options["header"] = headers
	}

	if taskConfig.EnableIntegrityScan {
		options["check-integrity"] = true
	}

	if taskConfig.VerifyHash {
		hashType := strings.ToLower(strings.TrimSpace(taskConfig.HashType))
		if _, ok := supportedChecksumAlgorithms[hashType]; !ok {
			return nil, fmt.Errorf("unsupported checksum algorithm: %s", hashType)
		}
		if taskConfig.ExpectedHash == "" {
			return nil, fmt.Errorf("checksum expected hash is required when verify_hash is enabled")
		}
		options["checksum"] = fmt.Sprintf("%s=%s", hashType, taskConfig.ExpectedHash)
		options["check-integrity"] = true
	}

	if err := applyOverwritePolicy(options, taskConfig.OverwritePolicy); err != nil {
		return nil, err
	}

	return options, nil
}

func applyOverwritePolicy(options aria2.Options, policy string) error {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "", "rename":
		options["auto-file-renaming"] = true
		options["allow-overwrite"] = false
	case "overwrite":
		options["auto-file-renaming"] = false
		options["allow-overwrite"] = true
	case "skip":
		options["auto-file-renaming"] = false
		options["allow-overwrite"] = false
	default:
		return fmt.Errorf("invalid overwrite policy: %s", policy)
	}
	return nil
}

func ApplyQueuePriority(ctx context.Context, client *aria2.Client, gid string, priority int) error {
	if client == nil || gid == "" || priority == 0 {
		return nil
	}

	var how string
	pos := 0

	if priority > 0 {
		how = "POS_SET"
	} else {
		how = "POS_END"
	}

	_, err := client.ChangePosition(ctx, gid, pos, how)
	return err
}
