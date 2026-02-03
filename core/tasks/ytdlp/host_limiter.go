package ytdlp

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

type hostLimiter struct {
	minDelay time.Duration
	maxDelay time.Duration
	jitter   float64
	mu       sync.Mutex
	hosts    map[string]*hostLimitState
	rng      *rand.Rand
}

type hostLimitState struct {
	delay   time.Duration
	lastHit time.Time
}

func newHostLimiter(minDelay, maxDelay time.Duration, jitter float64) *hostLimiter {
	if minDelay <= 0 && maxDelay <= 0 {
		return nil
	}
	if maxDelay > 0 && minDelay > maxDelay {
		minDelay, maxDelay = maxDelay, minDelay
	}
	if jitter < 0 {
		jitter = 0
	}
	return &hostLimiter{
		minDelay: minDelay,
		maxDelay: maxDelay,
		jitter:   jitter,
		hosts:    make(map[string]*hostLimitState),
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (h *hostLimiter) Wait(ctx context.Context, host string) error {
	if h == nil || host == "" {
		return nil
	}

	h.mu.Lock()
	state := h.stateForHost(host)
	waitFor := h.calculateWait(state)
	if waitFor > 0 {
		state.lastHit = time.Now().Add(waitFor)
	}
	h.mu.Unlock()

	if waitFor <= 0 {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(waitFor):
		return nil
	}
}

func (h *hostLimiter) Report(host string, class RetryClass, success bool) {
	if h == nil || host == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	state := h.stateForHost(host)
	if success {
		state.delay = h.reduceDelay(state.delay)
		return
	}

	switch class {
	case RetryRateLimit, RetryServerError, RetryNetworkError:
		state.delay = h.increaseDelay(state.delay)
	}
}

func (h *hostLimiter) stateForHost(host string) *hostLimitState {
	if state, ok := h.hosts[host]; ok {
		return state
	}
	delay := h.minDelay
	if delay < 0 {
		delay = 0
	}
	state := &hostLimitState{delay: delay}
	h.hosts[host] = state
	return state
}

func (h *hostLimiter) calculateWait(state *hostLimitState) time.Duration {
	if state.delay <= 0 {
		return 0
	}
	now := time.Now()
	if state.lastHit.IsZero() {
		state.lastHit = now
		return 0
	}
	next := state.lastHit.Add(state.delay)
	if next.After(now) {
		wait := next.Sub(now)
		if h.jitter > 0 {
			jitterRange := float64(wait) * h.jitter
			wait += time.Duration(h.rng.Float64() * jitterRange)
		}
		return wait
	}
	state.lastHit = now
	return 0
}

func (h *hostLimiter) increaseDelay(current time.Duration) time.Duration {
	if current <= 0 {
		return h.minDelay
	}
	next := current * 2
	if h.maxDelay > 0 && next > h.maxDelay {
		return h.maxDelay
	}
	return next
}

func (h *hostLimiter) reduceDelay(current time.Duration) time.Duration {
	if current <= 0 {
		return h.minDelay
	}
	next := current / 2
	if next < h.minDelay {
		return h.minDelay
	}
	return next
}
