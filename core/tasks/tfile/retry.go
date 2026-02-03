package tfile

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/charmbracelet/log"
)

type retryConfig struct {
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
	jitter     float64
}

func retryWithBackoff(ctx context.Context, logger *log.Logger, cfg retryConfig, action func(int) error) error {
	if cfg.maxRetries < 1 {
		cfg.maxRetries = 1
	}
	if cfg.baseDelay <= 0 {
		cfg.baseDelay = 500 * time.Millisecond
	}
	if cfg.maxDelay <= 0 {
		cfg.maxDelay = 10 * time.Second
	}
	if cfg.jitter < 0 {
		cfg.jitter = 0
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	var lastErr error
	for attempt := 1; attempt <= cfg.maxRetries; attempt++ {
		if err := action(attempt); err != nil {
			lastErr = err
			if attempt == cfg.maxRetries {
				return err
			}
			delay := backoffDuration(cfg, attempt, rng)
			if logger != nil {
				logger.Warnf("Attempt %d failed: %v (retrying in %s)", attempt, err, delay)
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("retry exhausted: %w", lastErr)
}

func backoffDuration(cfg retryConfig, attempt int, rng *rand.Rand) time.Duration {
	pow := math.Pow(2, float64(attempt-1))
	delay := time.Duration(float64(cfg.baseDelay) * pow)
	if cfg.maxDelay > 0 && delay > cfg.maxDelay {
		delay = cfg.maxDelay
	}
	if cfg.jitter > 0 && rng != nil {
		jitter := (rng.Float64()*2 - 1) * cfg.jitter
		delay = time.Duration(float64(delay) * (1 + jitter))
		if delay < 0 {
			delay = 0
		}
	}
	return delay
}
