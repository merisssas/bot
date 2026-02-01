package directlinks

import (
	"context"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/merisssas/Bot/config"
)

type overwritePolicy string

const (
	overwritePolicyOverwrite overwritePolicy = "overwrite"
	overwritePolicyRename    overwritePolicy = "rename"
	overwritePolicySkip      overwritePolicy = "skip"
)

func parseOverwritePolicy(policy string) overwritePolicy {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case string(overwritePolicyOverwrite):
		return overwritePolicyOverwrite
	case string(overwritePolicySkip):
		return overwritePolicySkip
	default:
		return overwritePolicyRename
	}
}

type retryPolicy struct {
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
}

func newRetryPolicy(maxRetries int, baseDelay time.Duration, maxDelay time.Duration) retryPolicy {
	if maxRetries < 1 {
		maxRetries = 1
	}
	if maxRetries < 1 {
		maxRetries = 1
	}
	if baseDelay <= 0 {
		baseDelay = 500 * time.Millisecond
	}
	if maxDelay <= 0 {
		maxDelay = 10 * time.Second
	}
	return retryPolicy{
		maxRetries: maxRetries,
		baseDelay:  baseDelay,
		maxDelay:   maxDelay,
	}
}

type segmentConfig struct {
	maxConcurrency int
	minFileSize    int64
	minSegmentSize int64
}

func newSegmentConfig(segmentConcurrency int, minMultipartSize string, minSegmentSize string) segmentConfig {
	minFileSize := int64(5 * 1024 * 1024)
	if parsed, ok := parseByteSize(minMultipartSize); ok {
		minFileSize = parsed
	}
	minSegment := int64(1 * 1024 * 1024)
	if parsed, ok := parseByteSize(minSegmentSize); ok {
		minSegment = parsed
	}
	concurrency := segmentConcurrency
	if concurrency < 1 {
		concurrency = 8
	}
	return segmentConfig{
		maxConcurrency: concurrency,
		minFileSize:    minFileSize,
		minSegmentSize: minSegment,
	}
}

func (t *Task) shouldDisableStream() bool {
	if !t.stream {
		return false
	}
	cfg := config.C().Directlinks
	if cfg.EnableResume || cfg.ChecksumAlgorithm != "" || cfg.ExpectedChecksum != "" {
		return true
	}
	return false
}

func (t *Task) closeLogger() {
	if t.logFile != nil {
		_ = t.logFile.Close()
	}
}

func buildTaskLogger(ctx context.Context, logFilePath, logLevel string) (*log.Logger, *os.File) {
	if strings.TrimSpace(logFilePath) == "" {
		return log.FromContext(ctx), nil
	}

	level := parseLogLevel(logLevel)
	file, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		logger := log.FromContext(ctx)
		logger.Errorf("Failed to open directlinks log file %s: %v", logFilePath, err)
		return logger, nil
	}
	writer := io.MultiWriter(os.Stdout, file)
	logger := log.NewWithOptions(writer, log.Options{
		Level:           level,
		ReportTimestamp: true,
		TimeFormat:      time.TimeOnly,
		ReportCaller:    false,
	})
	return logger, file
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
