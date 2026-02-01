package directlinks

import (
	"context"
	"sync"
	"time"
)

type rateLimiter struct {
	rateBytes  int64
	burstBytes int64
	available  int64
	last       time.Time
	mu         sync.Mutex
}

func newRateLimiter(rateStr, burstStr string) *rateLimiter {
	rate, ok := parseByteSize(rateStr)
	if !ok || rate <= 0 {
		return nil
	}
	burst, ok := parseByteSize(burstStr)
	if !ok || burst <= 0 {
		burst = rate
	}
	return &rateLimiter{
		rateBytes:  rate,
		burstBytes: burst,
		available:  burst,
		last:       time.Now(),
	}
}

func (rl *rateLimiter) Wait(ctx context.Context, n int64) error {
	if rl == nil || n <= 0 {
		return nil
	}

	for {
		rl.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(rl.last)
		if elapsed > 0 {
			rl.available += int64(float64(rl.rateBytes) * elapsed.Seconds())
			if rl.available > rl.burstBytes {
				rl.available = rl.burstBytes
			}
			rl.last = now
		}

		if rl.available >= n {
			rl.available -= n
			rl.mu.Unlock()
			return nil
		}

		needed := n - rl.available
		waitDuration := time.Duration(float64(needed)/float64(rl.rateBytes)*float64(time.Second)) + 10*time.Millisecond
		rl.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitDuration):
		}
	}
}
