package ytdlp

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/log"

	"github.com/merisssas/Bot/config"
)

type OverwritePolicy string

const (
	OverwritePolicyOverwrite OverwritePolicy = "overwrite"
	OverwritePolicyRename    OverwritePolicy = "rename"
	OverwritePolicySkip      OverwritePolicy = "skip"
)

type TaskConfig struct {
	MaxRetries          int
	RetryBaseDelay      time.Duration
	RetryMaxDelay       time.Duration
	RetryJitter         float64
	DownloadConcurrency int
	FragmentConcurrency int
	EnableResume        bool
	Proxy               string
	ProxyPool           []string

	ExternalDownloader    string
	ExternalDownloaderArg []string

	LimitRate     string
	ThrottledRate string
	AdaptiveLimit bool
	AdaptiveMin   string
	AdaptiveMax   string

	OverwritePolicy   OverwritePolicy
	FormatSort        string
	FormatFallbacks   []string
	RecodeVideo       string
	MergeOutputFormat string
	EnableRepair      bool
	RepairPasses      int
	DedupEnabled      bool
	PersistState      bool
	StateDir          string
	CleanupState      bool
	RateLimitMinDelay time.Duration
	RateLimitMaxDelay time.Duration
	RateLimitJitter   float64
	FingerprintRandom bool
	DryRun            bool
	ChecksumAlgorithm string
	ExpectedChecksum  string
	WriteChecksumFile bool

	LogFile  string
	LogLevel log.Level

	UserAgent     string
	UserAgentPool []string
	HappyEyeballs bool
	Priority      int
}

type Option func(*Task)

func WithConfig(cfg TaskConfig) Option {
	return func(t *Task) {
		t.Config = cfg
	}
}

func WithPriority(priority int) Option {
	return func(t *Task) {
		t.Config.Priority = priority
	}
}

func WithDryRun(enabled bool) Option {
	return func(t *Task) {
		t.Config.DryRun = enabled
	}
}

func WithOverwritePolicy(policy OverwritePolicy) Option {
	return func(t *Task) {
		t.Config.OverwritePolicy = policy
	}
}

func WithFormatSort(formatSort string) Option {
	return func(t *Task) {
		t.Config.FormatSort = formatSort
	}
}

func WithRecodeVideo(recode string) Option {
	return func(t *Task) {
		t.Config.RecodeVideo = recode
	}
}

func WithMergeOutputFormat(format string) Option {
	return func(t *Task) {
		t.Config.MergeOutputFormat = format
	}
}

func WithChecksum(algorithm, expected string, writeFile bool) Option {
	return func(t *Task) {
		t.Config.ChecksumAlgorithm = algorithm
		t.Config.ExpectedChecksum = expected
		t.Config.WriteChecksumFile = writeFile
	}
}

func WithProxy(proxy string) Option {
	return func(t *Task) {
		t.Config.Proxy = proxy
	}
}

func WithLimitRate(limit string) Option {
	return func(t *Task) {
		t.Config.LimitRate = limit
	}
}

func WithDownloadConcurrency(concurrency int) Option {
	return func(t *Task) {
		t.Config.DownloadConcurrency = concurrency
	}
}

func WithFragmentConcurrency(concurrency int) Option {
	return func(t *Task) {
		t.Config.FragmentConcurrency = concurrency
	}
}

func defaultTaskConfig() TaskConfig {
	cfg := config.C()

	return TaskConfig{
		MaxRetries:            cfg.Ytdlp.MaxRetries,
		RetryBaseDelay:        cfg.Ytdlp.RetryBaseDelay,
		RetryMaxDelay:         cfg.Ytdlp.RetryMaxDelay,
		RetryJitter:           cfg.Ytdlp.RetryJitter,
		DownloadConcurrency:   cfg.Ytdlp.DownloadConcurrency,
		FragmentConcurrency:   cfg.Ytdlp.FragmentConcurrency,
		EnableResume:          cfg.Ytdlp.EnableResume,
		Proxy:                 firstNonEmpty(cfg.Ytdlp.Proxy, cfg.Proxy),
		ProxyPool:             cfg.Ytdlp.ProxyPool,
		ExternalDownloader:    cfg.Ytdlp.ExternalDownloader,
		ExternalDownloaderArg: cfg.Ytdlp.ExternalDownloaderArg,
		LimitRate:             cfg.Ytdlp.LimitRate,
		ThrottledRate:         cfg.Ytdlp.ThrottledRate,
		AdaptiveLimit:         cfg.Ytdlp.AdaptiveLimit,
		AdaptiveMin:           cfg.Ytdlp.AdaptiveLimitMinRate,
		AdaptiveMax:           cfg.Ytdlp.AdaptiveLimitMaxRate,
		OverwritePolicy:       parseOverwritePolicy(cfg.Ytdlp.OverwritePolicy),
		FormatSort:            cfg.Ytdlp.FormatSort,
		FormatFallbacks:       cfg.Ytdlp.FormatFallbacks,
		RecodeVideo:           cfg.Ytdlp.RecodeVideo,
		MergeOutputFormat:     cfg.Ytdlp.MergeOutputFormat,
		EnableRepair:          cfg.Ytdlp.EnableFragmentRepair,
		RepairPasses:          cfg.Ytdlp.FragmentRepairPasses,
		DedupEnabled:          cfg.Ytdlp.DedupEnabled,
		PersistState:          cfg.Ytdlp.PersistState,
		StateDir:              cfg.Ytdlp.StateDir,
		CleanupState:          cfg.Ytdlp.CleanupStateOnSuccess,
		RateLimitMinDelay:     cfg.Ytdlp.RateLimitMinInterval,
		RateLimitMaxDelay:     cfg.Ytdlp.RateLimitMaxInterval,
		RateLimitJitter:       cfg.Ytdlp.RateLimitJitter,
		FingerprintRandom:     cfg.Ytdlp.FingerprintRandomize,
		DryRun:                cfg.Ytdlp.DryRun,
		ChecksumAlgorithm:     cfg.Ytdlp.ChecksumAlgorithm,
		ExpectedChecksum:      cfg.Ytdlp.ExpectedChecksum,
		WriteChecksumFile:     cfg.Ytdlp.WriteChecksumFile,
		LogFile:               cfg.Ytdlp.LogFile,
		LogLevel:              parseLogLevel(cfg.Ytdlp.LogLevel),
		UserAgent:             cfg.Ytdlp.UserAgent,
		UserAgentPool:         cfg.Ytdlp.UserAgentPool,
		HappyEyeballs:         cfg.Ytdlp.HappyEyeballs,
		Priority:              1,
	}
}

func parseOverwritePolicy(policy string) OverwritePolicy {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case string(OverwritePolicyOverwrite):
		return OverwritePolicyOverwrite
	case string(OverwritePolicySkip):
		return OverwritePolicySkip
	default:
		return OverwritePolicyRename
	}
}

func parseLogLevel(level string) log.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return log.DebugLevel
	case "warn", "warning":
		return log.WarnLevel
	case "error":
		return log.ErrorLevel
	default:
		return log.InfoLevel
	}
}

func validateConfig(cfg TaskConfig) error {
	if cfg.DownloadConcurrency < 1 {
		return fmt.Errorf("download concurrency must be >= 1")
	}
	if cfg.FragmentConcurrency < 1 {
		return fmt.Errorf("fragment concurrency must be >= 1")
	}
	if cfg.MaxRetries < 0 {
		return fmt.Errorf("max retries must be >= 0")
	}
	if cfg.RetryJitter < 0 || cfg.RetryJitter > 1 {
		return fmt.Errorf("retry jitter must be between 0 and 1")
	}
	if cfg.RetryBaseDelay < 0 {
		return fmt.Errorf("retry base delay must be >= 0")
	}
	if cfg.RetryMaxDelay < 0 {
		return fmt.Errorf("retry max delay must be >= 0")
	}
	if cfg.RateLimitJitter < 0 || cfg.RateLimitJitter > 1 {
		return fmt.Errorf("rate limit jitter must be between 0 and 1")
	}
	if cfg.RepairPasses < 0 {
		return fmt.Errorf("fragment repair passes must be >= 0")
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
