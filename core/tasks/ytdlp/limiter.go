package ytdlp

import (
	"context"
	"fmt"
	"sync"

	"github.com/merisssas/bot/config"
)

var (
	downloadLimiterOnce sync.Once
	downloadLimiter     chan struct{}
)

func acquireDownloadSlot(ctx context.Context) error {
	downloadLimiterOnce.Do(func() {
		limit := config.C().Ytdlp.MaxConcurrentDownloads
		if limit < 1 {
			limit = 1
		}
		downloadLimiter = make(chan struct{}, limit)
	})

	select {
	case downloadLimiter <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("download queue canceled: %w", ctx.Err())
	}
}

func releaseDownloadSlot() {
	if downloadLimiter == nil {
		return
	}
	select {
	case <-downloadLimiter:
	default:
	}
}
