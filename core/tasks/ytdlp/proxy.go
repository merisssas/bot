package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"

	"github.com/merisssas/bot/config"
)

type proxyCache struct {
	mu        sync.Mutex
	proxies   []string
	fetchedAt time.Time
}

var ytdlpProxyCache proxyCache

func appendProxyArgs(ctx context.Context, logger *log.Logger, args []string, urls []string, flags []string) []string {
	if hasProxyFlag(flags) {
		return args
	}

	proxyCfg := config.C().Ytdlp.Proxy
	if !proxyCfg.Enable || len(proxyCfg.Sources) == 0 || !shouldUseYtdlpProxy(urls) {
		return args
	}

	proxyURL, err := selectYtdlpProxy(ctx, logger, proxyCfg)
	if err != nil {
		logger.Warnf("Failed to fetch yt-dlp proxy list: %v", err)
		return args
	}
	if proxyURL == "" {
		return args
	}

	logger.Debugf("Selected yt-dlp proxy %s", proxyURL)
	return append(args, "--proxy", proxyURL)
}

func hasProxyFlag(flags []string) bool {
	for _, flag := range flags {
		if strings.HasPrefix(flag, "--proxy") {
			return true
		}
	}
	return false
}

func shouldUseYtdlpProxy(urls []string) bool {
	for _, rawURL := range urls {
		if isYouTubeURL(rawURL) {
			return true
		}
	}
	return false
}

func isYouTubeURL(rawURL string) bool {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return false
	}

	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Host != "" {
		host := strings.ToLower(parsed.Host)
		return strings.Contains(host, "youtube.com") ||
			strings.Contains(host, "youtu.be") ||
			strings.Contains(host, "youtube-nocookie.com")
	}

	lower := strings.ToLower(rawURL)
	return strings.Contains(lower, "youtube.com") || strings.Contains(lower, "youtu.be")
}

func selectYtdlpProxy(ctx context.Context, logger *log.Logger, cfg config.YtdlpProxyConfig) (string, error) {
	proxies, err := fetchProxyList(ctx, logger, cfg)
	if err != nil {
		return "", err
	}
	if len(proxies) == 0 {
		return "", errors.New("proxy list is empty")
	}
	return proxies[rand.Intn(len(proxies))], nil
}

func fetchProxyList(ctx context.Context, logger *log.Logger, cfg config.YtdlpProxyConfig) ([]string, error) {
	refresh := time.Duration(cfg.RefreshMinutes) * time.Minute
	if refresh <= 0 {
		refresh = 30 * time.Minute
	}

	ytdlpProxyCache.mu.Lock()
	if len(ytdlpProxyCache.proxies) > 0 && time.Since(ytdlpProxyCache.fetchedAt) < refresh {
		proxies := append([]string(nil), ytdlpProxyCache.proxies...)
		ytdlpProxyCache.mu.Unlock()
		return proxies, nil
	}
	ytdlpProxyCache.mu.Unlock()

	proxies, err := downloadProxySources(ctx, logger, cfg.Sources)
	if err != nil {
		return nil, err
	}
	if len(proxies) == 0 {
		return nil, errors.New("no valid proxies found")
	}

	ytdlpProxyCache.mu.Lock()
	ytdlpProxyCache.proxies = proxies
	ytdlpProxyCache.fetchedAt = time.Now()
	ytdlpProxyCache.mu.Unlock()

	return proxies, nil
}

func downloadProxySources(ctx context.Context, logger *log.Logger, sources []string) ([]string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	unique := make(map[string]struct{})

	for _, source := range sources {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}

		u, err := url.Parse(source)
		if err != nil || u.Scheme == "" || u.Host == "" {
			logger.Warnf("Skipping invalid proxy source: %s", source)
			continue
		}
		host := strings.ToLower(u.Host)
		if !strings.Contains(host, "github.com") && !strings.Contains(host, "githubusercontent.com") {
			logger.Warnf("Skipping non-GitHub proxy source: %s", source)
			continue
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			logger.Warnf("Failed to build request for proxy source %s: %v", source, err)
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			logger.Warnf("Failed to fetch proxy source %s: %v", source, err)
			continue
		}
		body, err := readLimitedBody(resp.Body, 2*1024*1024)
		resp.Body.Close()
		if err != nil {
			logger.Warnf("Failed to read proxy source %s: %v", source, err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			logger.Warnf("Proxy source %s responded with status %d", source, resp.StatusCode)
			continue
		}

		for _, line := range strings.Split(string(body), "\n") {
			if proxyURL, ok := normalizeProxyLine(line); ok {
				unique[proxyURL] = struct{}{}
			}
		}
	}

	if len(unique) == 0 {
		return nil, errors.New("no proxies parsed from sources")
	}

	proxies := make([]string, 0, len(unique))
	for proxyURL := range unique {
		proxies = append(proxies, proxyURL)
	}
	return proxies, nil
}

func normalizeProxyLine(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", false
	}

	if strings.Contains(line, "://") {
		parsed, err := url.Parse(line)
		if err != nil || parsed.Host == "" {
			return "", false
		}
		return parsed.String(), true
	}

	if strings.Contains(line, "/") {
		return "", false
	}
	if strings.Count(line, ":") < 1 {
		return "", false
	}

	parsed, err := url.Parse("http://" + line)
	if err != nil || parsed.Host == "" {
		return "", false
	}
	return parsed.String(), true
}

func readLimitedBody(body io.ReadCloser, limit int64) ([]byte, error) {
	limited := &io.LimitedReader{R: body, N: limit + 1}
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("proxy list exceeds size limit (%d bytes)", limit)
	}
	return data, nil
}
