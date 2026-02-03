package ytdlp

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/log"
)

func (t *Task) initResilience() {
	if t.Config.PersistState && t.stateManager == nil {
		stateFile := filepath.Join(t.Config.StateDir, "state.json")
		t.stateManager = newStateManager(stateFile)
	}
	if t.rateLimiter == nil {
		t.rateLimiter = newHostLimiter(t.Config.RateLimitMinDelay, t.Config.RateLimitMaxDelay, t.Config.RateLimitJitter)
	}
	if t.proxyPool == nil {
		proxies := append([]string{}, t.Config.ProxyPool...)
		if t.Config.Proxy != "" {
			proxies = append(proxies, t.Config.Proxy)
		}
		t.proxyPool = newProxyPool(proxies)
	}
	if t.deduper == nil {
		t.deduper = newDeduper(t.Config.DedupEnabled)
	}
	if t.rateController == nil {
		t.rateController = newAdaptiveRateController(t.Config.AdaptiveLimit, t.Config.AdaptiveMin, t.Config.AdaptiveMax, t.Config.LimitRate)
	}
	if t.uaRand == nil {
		t.uaRand = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
}

func (t *Task) resolveTaskDir(basePath string) (string, bool, error) {
	if t.Config.PersistState {
		if err := os.MkdirAll(t.Config.StateDir, 0755); err != nil {
			return "", false, err
		}
		taskDir := filepath.Join(t.Config.StateDir, t.ID)
		if err := os.MkdirAll(taskDir, 0755); err != nil {
			return "", false, err
		}
		return taskDir, t.Config.CleanupState, nil
	}

	taskDir, err := os.MkdirTemp(basePath, "ytdlp-task-*")
	if err != nil {
		return "", false, err
	}
	return taskDir, true, nil
}

func (t *Task) prepareURLDir(taskDir string, index int) (string, bool, error) {
	if t.Config.PersistState {
		subDir := filepath.Join(taskDir, fmt.Sprintf("url-%d", index+1))
		if err := os.MkdirAll(subDir, 0755); err != nil {
			return "", false, newTaskError(ErrorCodeWorkspace, "create url workspace", err)
		}
		return subDir, false, nil
	}

	subDir, err := os.MkdirTemp(taskDir, fmt.Sprintf("url-%d-*", index+1))
	if err != nil {
		return "", false, newTaskError(ErrorCodeWorkspace, "create url workspace", err)
	}
	return subDir, true, nil
}

func (t *Task) waitForRateLimit(ctx context.Context, host string) error {
	if t.rateLimiter == nil {
		return nil
	}
	if err := t.rateLimiter.Wait(ctx, host); err != nil {
		return newTaskError(ErrorCodeCanceled, "rate limit wait", err)
	}
	return nil
}

func (t *Task) pickProxy() string {
	if t.proxyPool != nil {
		return t.proxyPool.Pick()
	}
	return t.Config.Proxy
}

func (t *Task) pickIPMode(attempt int) string {
	if !t.Config.HappyEyeballs {
		return ""
	}
	switch attempt % 3 {
	case 1:
		return "4"
	case 2:
		return "6"
	default:
		return ""
	}
}

func (t *Task) pickUserAgent() string {
	if !t.Config.FingerprintRandom {
		return t.Config.UserAgent
	}
	if len(t.Config.UserAgentPool) == 0 {
		return t.Config.UserAgent
	}
	return t.Config.UserAgentPool[t.uaRand.Intn(len(t.Config.UserAgentPool))]
}

func (t *Task) pickHeaders() []string {
	if !t.Config.FingerprintRandom {
		return nil
	}
	headers := []string{
		"Accept-Language: en-US,en;q=0.9",
		"DNT: 1",
		"Sec-Fetch-Site: none",
		"Sec-Fetch-Mode: navigate",
		"Sec-Fetch-Dest: document",
	}
	return headers
}

func (t *Task) pickLimitRate() string {
	if t.rateController == nil {
		return ""
	}
	return t.rateController.CurrentLimit()
}

func (t *Task) filterDuplicates(logger *log.Logger, files []string) ([]string, error) {
	if t.deduper == nil {
		return files, nil
	}
	filtered := make([]string, 0, len(files))
	for _, file := range files {
		duplicate, existing, err := t.deduper.IsDuplicate(file)
		if err != nil {
			return nil, err
		}
		if duplicate {
			logger.Warnf("Skipping duplicate file %s (matches %s)", file, existing)
			continue
		}
		filtered = append(filtered, file)
	}
	return filtered, nil
}
