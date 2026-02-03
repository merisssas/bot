package ytdlp

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/charmbracelet/log"
)

func (t *Task) repairFragments(ctx context.Context, logger *log.Logger, tempDir string, item queueItem, plan downloadPlan) ([]string, error) {
	if !t.Config.EnableRepair || t.Config.RepairPasses <= 0 {
		return nil, nil
	}

	for pass := 1; pass <= t.Config.RepairPasses; pass++ {
		logger.Warnf("🧩 Repairing fragments (pass %d/%d) for %s", pass, t.Config.RepairPasses, item.url)
		cmd := t.buildCommand().
			Output(filepath.Join(tempDir, "%(title).80s-%(id)s.%(ext)s")).
			RestrictFilenames().
			Continue().
			ConcurrentFragments(1).
			FragmentRetries("20").
			Retries("10").
			FileAccessRetries("10").
			RetrySleep("fragment:exp=1:20").
			RetrySleep("http:exp=1:10")

		if plan.proxy != "" {
			cmd.Proxy(plan.proxy)
		}
		if plan.userAgent != "" {
			cmd.UserAgent(plan.userAgent)
		}
		for _, header := range plan.headers {
			cmd.AddHeaders(header)
		}

		t.configureProgress(ctx, cmd, item, plan)
		args := append(t.Flags, item.url)
		_, runErr := cmd.Run(ctx, args...)
		if runErr != nil {
			logger.Warnf("Fragment repair attempt failed: %v", runErr)
		}

		files, err := collectValidFiles(logger, tempDir)
		if err != nil {
			return nil, err
		}
		if len(files) > 0 {
			return files, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	return nil, fmt.Errorf("fragment repair exhausted for %s", item.url)
}
