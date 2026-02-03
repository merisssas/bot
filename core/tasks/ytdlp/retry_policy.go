package ytdlp

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

type RetryClass int

const (
	RetryUnknown RetryClass = iota
	RetryRateLimit
	RetryServerError
	RetryNetworkError
)

type RetryPolicy struct {
	MaxAttempts            int
	BaseDelay              time.Duration
	MaxDelay               time.Duration
	JitterFactor           float64
	RateLimitMultiplier    float64
	ServerErrorMultiplier  float64
	NetworkErrorMultiplier float64
}

func (p RetryPolicy) NextDelay(attempt int, class RetryClass) (time.Duration, bool) {
	if attempt >= p.MaxAttempts {
		return 0, false
	}

	delay := p.BaseDelay
	if delay <= 0 {
		delay = 500 * time.Millisecond
	}

	if attempt > 0 {
		delay *= time.Duration(1 << uint(attempt-1))
	}

	switch class {
	case RetryRateLimit:
		delay = time.Duration(float64(delay) * p.RateLimitMultiplier)
	case RetryServerError:
		delay = time.Duration(float64(delay) * p.ServerErrorMultiplier)
	case RetryNetworkError:
		delay = time.Duration(float64(delay) * p.NetworkErrorMultiplier)
	}

	if p.MaxDelay > 0 && delay > p.MaxDelay {
		delay = p.MaxDelay
	}

	return delay, true
}

type retryableError interface {
	RetryClass() RetryClass
}

var (
	statusCodeRegex = regexp.MustCompile(`\b(4\d\d|5\d\d)\b`)
	rateLimitRegex  = regexp.MustCompile(`(?i)\b(429|too many requests|rate limit)\b`)
)

func classifyRetryError(err error) RetryClass {
	if err == nil {
		return RetryUnknown
	}

	var retryErr retryableError
	if errors.As(err, &retryErr) {
		return retryErr.RetryClass()
	}

	msg := strings.ToLower(err.Error())
	if rateLimitRegex.MatchString(msg) {
		return RetryRateLimit
	}

	matches := statusCodeRegex.FindStringSubmatch(msg)
	if len(matches) > 1 {
		code := matches[1]
		if strings.HasPrefix(code, "5") {
			return RetryServerError
		}
		if code == "429" {
			return RetryRateLimit
		}
	}

	if strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "temporary failure") ||
		strings.Contains(msg, "eof") {
		return RetryNetworkError
	}

	return RetryUnknown
}

type retryContextError struct {
	class RetryClass
	err   error
}

func (e *retryContextError) Error() string {
	return e.err.Error()
}

func (e *retryContextError) Unwrap() error {
	return e.err
}

func (e *retryContextError) RetryClass() RetryClass {
	return e.class
}
