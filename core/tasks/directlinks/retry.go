package directlinks

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

type jitterSource struct {
	mu  sync.Mutex
	rng *rand.Rand
}

func newJitterSource() *jitterSource {
	return &jitterSource{
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (j *jitterSource) jitter(d time.Duration) time.Duration {
	j.mu.Lock()
	defer j.mu.Unlock()
	factor := 0.7 + (j.rng.Float64() * 0.6)
	return time.Duration(float64(d) * factor)
}

func retryWithBackoff(ctx context.Context, policy retryPolicy, jitter *jitterSource, fn func() error) error {
	var err error
	for attempt := 0; attempt < policy.maxRetries; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err = fn()
		if err == nil {
			return nil
		}
		if attempt == policy.maxRetries-1 {
			break
		}
		delay := policy.baseDelay << attempt
		if delay > policy.maxDelay {
			delay = policy.maxDelay
		}
		if jitter != nil {
			delay = jitter.jitter(delay)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return err
}
