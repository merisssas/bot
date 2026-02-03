package ytdlp

import (
	"sync"
	"time"
)

type adaptiveRateController struct {
	minRate   int64
	maxRate   int64
	current   int64
	enabled   bool
	mu        sync.Mutex
	lastEvent time.Time
}

func newAdaptiveRateController(enabled bool, minRateStr, maxRateStr string, baseRateStr string) *adaptiveRateController {
	if !enabled {
		return nil
	}
	minRate, _ := parseByteSize(minRateStr)
	maxRate, _ := parseByteSize(maxRateStr)
	baseRate, _ := parseByteSize(baseRateStr)

	if baseRate <= 0 {
		baseRate = maxRate
	}
	if baseRate <= 0 {
		baseRate = minRate
	}
	if baseRate <= 0 {
		baseRate = 0
	}
	return &adaptiveRateController{
		minRate: minRate,
		maxRate: maxRate,
		current: baseRate,
		enabled: true,
	}
}

func (a *adaptiveRateController) CurrentLimit() string {
	if a == nil || !a.enabled {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.current <= 0 {
		return ""
	}
	return formatByteSize(a.current)
}

func (a *adaptiveRateController) Report(success bool, class RetryClass) {
	if a == nil || !a.enabled {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	if now.Sub(a.lastEvent) < 500*time.Millisecond {
		return
	}
	a.lastEvent = now

	if success {
		a.current = clampRate(a.current+int64(float64(a.current)*0.15), a.minRate, a.maxRate)
		return
	}

	if class == RetryRateLimit || class == RetryNetworkError {
		a.current = clampRate(int64(float64(a.current)*0.7), a.minRate, a.maxRate)
	}
}

func clampRate(value, min, max int64) int64 {
	if max > 0 && value > max {
		return max
	}
	if min > 0 && value < min {
		return min
	}
	return value
}
