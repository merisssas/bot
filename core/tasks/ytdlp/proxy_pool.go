package ytdlp

import (
	"math/rand"
	"sort"
	"sync"
	"time"
)

type proxyPool struct {
	mu       sync.Mutex
	proxies  map[string]*proxyScore
	order    []string
	rng      *rand.Rand
	cooldown time.Duration
}

type proxyScore struct {
	score         float64
	successes     int
	failures      int
	lastLatency   time.Duration
	lastUsed      time.Time
	cooldownUntil time.Time
}

func newProxyPool(proxies []string) *proxyPool {
	deduped := make([]string, 0, len(proxies))
	seen := make(map[string]struct{}, len(proxies))
	for _, proxy := range proxies {
		if proxy == "" {
			continue
		}
		if _, ok := seen[proxy]; ok {
			continue
		}
		seen[proxy] = struct{}{}
		deduped = append(deduped, proxy)
	}

	if len(deduped) == 0 {
		return nil
	}

	pool := &proxyPool{
		proxies:  make(map[string]*proxyScore, len(deduped)),
		order:    deduped,
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())),
		cooldown: 45 * time.Second,
	}
	for _, proxy := range deduped {
		pool.proxies[proxy] = &proxyScore{score: 1.0}
	}
	return pool
}

func (p *proxyPool) Pick() string {
	if p == nil {
		return ""
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	candidates := make([]string, 0, len(p.order))
	for _, proxy := range p.order {
		score := p.proxies[proxy]
		if score == nil {
			continue
		}
		if score.cooldownUntil.After(now) {
			continue
		}
		candidates = append(candidates, proxy)
	}

	if len(candidates) == 0 {
		candidates = append(candidates, p.order...)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return p.proxies[candidates[i]].score > p.proxies[candidates[j]].score
	})

	limit := 3
	if len(candidates) < limit {
		limit = len(candidates)
	}
	pick := candidates[p.rng.Intn(limit)]
	p.proxies[pick].lastUsed = now
	return pick
}

func (p *proxyPool) Report(proxy string, success bool, latency time.Duration) {
	if p == nil || proxy == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	score := p.proxies[proxy]
	if score == nil {
		return
	}

	if success {
		score.successes++
		score.score = clampFloat(score.score+0.2, 0.2, 5.0)
		if latency > 0 {
			score.lastLatency = latency
		}
		return
	}

	score.failures++
	score.score = clampFloat(score.score-0.4, 0.1, 5.0)
	score.cooldownUntil = time.Now().Add(p.cooldown)
}

func clampFloat(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
