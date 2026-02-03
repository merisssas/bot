package aria2dl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/log"
	"github.com/merisssas/Bot/config"
	"github.com/merisssas/Bot/pkg/aria2"
	"github.com/merisssas/Bot/pkg/enums/ctxkey"
	"golang.org/x/sync/errgroup"
)

const (
	maxConcurrentTransfers = 8
	transferBufferSize     = 4 * 1024 * 1024
	maxRetriesDefault      = 5
	defaultPollInterval    = 2 * time.Second
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// Execute implements core.Executable.
func (t *Task) Execute(ctx context.Context) (err error) {
	logger := log.FromContext(ctx).With("gid", t.GID(), "task", t.ID)
	logger.Info("Starting aria2 execution pipeline")

	if t.Progress != nil {
		t.Progress.OnStart(ctx, t)
		defer func() {
			t.Progress.OnDone(ctx, t, err)
		}()
	}

	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, rmErr := t.Aria2Client.RemoveDownloadResult(cleanupCtx, t.GID()); rmErr != nil {
			logger.Debug("Failed to remove aria2 download result (possibly already removed)", "err", rmErr)
		}
	}()

	if t.Config.DryRun {
		return t.executeDryRun(ctx)
	}

	if err := t.applyTrafficShaping(ctx); err != nil {
		logger.Warn("Failed to apply traffic shaping rules", "err", err)
	}

	if err := t.monitorDownloadLoop(ctx, logger); err != nil {
		if errors.Is(err, context.Canceled) {
			logger.Info("Task cancelled by user")
			t.cancelAria2Task(context.Background())
		}
		return err
	}

	if err := t.transferFilesParallel(ctx, logger); err != nil {
		return fmt.Errorf("transfer phase failed: %w", err)
	}

	logger.Info("Task completed successfully")
	return nil
}

// monitorDownloadLoop waits for aria2 to complete the download.
func (t *Task) monitorDownloadLoop(ctx context.Context, logger *log.Logger) error {
	ticker := time.NewTicker(defaultPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			status, err := t.fetchStatus(ctx)
			if err != nil {
				logger.Warn("Temporary status check failure", "err", err)
				continue
			}

			t.reportProgress(ctx, status)

			// Check if download is complete
			if status.IsDownloadComplete() {
				// Handle metadata downloads (torrent/magnet) that spawn follow-up downloads
				if len(status.FollowedBy) > 0 {
					newGID := status.FollowedBy[0]
					logger.Info("Metadata acquired, switching to payload download", "old_gid", t.GID(), "new_gid", newGID)
					t.SetGID(newGID)
					_, _ = t.Aria2Client.RemoveDownloadResult(ctx, status.GID)
					continue
				}
				logger.Info("Download completed")
				return nil
			}

			// Check for errors
			if status.IsDownloadError() {
				err := fmt.Errorf("aria2 download error: %s (code: %s)", status.ErrorMessage, status.ErrorCode)
				t.SetError(err)
				return err
			}

			if status.IsDownloadRemoved() {
				return errors.New("aria2 download was removed")
			}
		}
	}
}

// fetchStatus retrieves the current status of the download.
func (t *Task) fetchStatus(ctx context.Context) (*aria2.Status, error) {
	keys := []string{
		"gid",
		"status",
		"totalLength",
		"completedLength",
		"downloadSpeed",
		"connections",
		"files",
		"followedBy",
		"errorCode",
		"errorMessage",
		"dir",
	}

	status, err := t.Aria2Client.TellStatus(ctx, t.GID(), keys...)
	if err == nil {
		return status, nil
	}

	stopped, stopErr := t.Aria2Client.TellStopped(ctx, 0, 100, keys...)
	if stopErr != nil {
		return nil, fmt.Errorf("failed to get aria2 status: %w", err)
	}

	for _, task := range stopped {
		if task.GID == t.GID() {
			return &task, nil
		}
	}

	return nil, fmt.Errorf("task GID %s not found: %w", t.GID(), err)
}

// transferFilesParallel transfers downloaded files from aria2 to storage.
func (t *Task) transferFilesParallel(ctx context.Context, logger *log.Logger) error {
	status, err := t.fetchStatus(ctx)
	if err != nil {
		return err
	}

	files := filterValidFiles(status.Files)
	if len(files) == 0 {
		return errors.New("no valid files in aria2 download")
	}

	commonDir := filepath.Dir(files[0].Path)
	if status.Dir != "" {
		commonDir = status.Dir
	}

	logger.Infof("Transferring %d file(s) to storage %s with up to %d concurrent workers", len(files), t.Storage.Name(), maxConcurrentTransfers)

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(maxConcurrentTransfers)

	var transferredCount atomic.Int32

	for _, file := range files {
		file := file
		group.Go(func() error {
			relPath, err := filepath.Rel(commonDir, file.Path)
			if err != nil {
				relPath = filepath.Base(file.Path)
			}
			relPath = filepath.ToSlash(relPath)

			if err := t.transferFileWithRetry(groupCtx, file.Path, relPath); err != nil {
				return fmt.Errorf("failed to transfer %s: %w", relPath, err)
			}

			transferredCount.Add(1)

			t.cleanupLocalFile(groupCtx, file.Path)
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return err
	}

	if transferredCount.Load() == 0 {
		return errors.New("no files were transferred")
	}

	return nil
}

func (t *Task) transferFileWithRetry(ctx context.Context, srcPath, destRelPath string) error {
	maxRetries := t.Config.MaxRetries
	if maxRetries <= 0 {
		maxRetries = maxRetriesDefault
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err := t.transferSingleFile(ctx, srcPath, destRelPath); err == nil {
			return nil
		} else if os.IsNotExist(err) {
			log.FromContext(ctx).Warn("Downloaded file not found", "path", srcPath)
			return nil
		}

		lastErr = err
		if attempt < maxRetries {
			backoff := t.calculateBackoff(attempt)
			retryCount := t.IncrementRetry()
			t.SetError(err)
			log.FromContext(ctx).Warn("Transfer failed, retrying", "file", destRelPath, "attempt", attempt+1, "max", maxRetries, "retry", retryCount, "next_retry", backoff, "err", err)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return fmt.Errorf("max retries reached for %s: %w", destRelPath, lastErr)
}

func (t *Task) transferSingleFile(ctx context.Context, srcPath, destRelPath string) error {
	logger := log.FromContext(ctx)

	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	fileInfo, err := f.Stat()
	if err != nil {
		return err
	}

	reader := bufio.NewReaderSize(f, transferBufferSize)
	ctx = context.WithValue(ctx, ctxkey.ContentLength, fileInfo.Size())

	destPath := filepath.Join(t.StorPath, destRelPath)
	logger.Infof("Transferring file %s to %s:%s (%s)", filepath.Base(srcPath), t.Storage.Name(), destPath, FormatBytes(fileInfo.Size()))

	if err := t.Storage.Save(ctx, reader, destPath); err != nil {
		return err
	}

	logger.Infof("Successfully transferred file %s", filepath.Base(srcPath))
	return nil
}

func (t *Task) executeDryRun(ctx context.Context) error {
	logger := log.FromContext(ctx)
	result, err := DryRun(ctx, t.Config, t.URIs)
	if err != nil {
		return err
	}

	for _, file := range result.Files {
		logger.Infof("Dry-run file: %s (%s, %d bytes, status=%d)", file.FileName, file.ContentType, file.Length, file.StatusCode)
	}
	for _, skipped := range result.Skipped {
		logger.Warnf("Dry-run skipped URI: %s", skipped)
	}

	return nil
}

func (t *Task) applyTrafficShaping(ctx context.Context) error {
	if t.Config.BurstRate == "" {
		return nil
	}

	_, err := t.Aria2Client.ChangeOption(ctx, t.GID(), aria2.Options{
		"max-download-limit": t.Config.BurstRate,
	})
	if err != nil {
		return err
	}

	if t.Config.LimitRate == "" || t.Config.BurstDuration <= 0 {
		return nil
	}

	time.AfterFunc(t.Config.BurstDuration, func() {
		changeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, changeErr := t.Aria2Client.ChangeOption(changeCtx, t.GID(), aria2.Options{
			"max-download-limit": t.Config.LimitRate,
		})
		if changeErr != nil {
			log.FromContext(ctx).Warnf("Failed to apply steady-state limit: %v", changeErr)
		}
	})

	return nil
}

// cleanupLocalFile removes a file if RemoveAfterTransfer is enabled.
func (t *Task) cleanupLocalFile(ctx context.Context, filePath string) {
	if !config.C().Aria2.RemoveAfterTransferEnabled() {
		return
	}

	logger := log.FromContext(ctx)
	if err := os.Remove(filePath); err != nil {
		logger.Warnf("Failed to remove local file %s: %v", filePath, err)
	} else {
		logger.Debugf("Removed local file %s", filePath)
	}

	parentDir := filepath.Dir(filePath)
	if err := os.Remove(parentDir); err != nil && !os.IsNotExist(err) {
		logger.Debugf("Failed to remove parent directory %s: %v", parentDir, err)
	}
}

// cancelAria2Task cancels the aria2 download task.
func (t *Task) cancelAria2Task(ctx context.Context) {
	if _, err := t.Aria2Client.ForceRemove(ctx, t.GID()); err != nil {
		log.FromContext(ctx).Warnf("Failed to cancel aria2 download %s: %v", t.GID(), err)
	}
}

func (t *Task) reportProgress(ctx context.Context, status *aria2.Status) {
	if t.Progress != nil {
		t.Progress.OnProgress(ctx, t, status)
	}
	t.UpdateStats(
		statusToInt64(status.CompletedLength),
		statusToInt64(status.TotalLength),
		statusToInt64(status.DownloadSpeed),
		statusToInt(status.Connections),
	)
}

func statusToInt64(value string) int64 {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func statusToInt(value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}

func (t *Task) calculateBackoff(attempt int) time.Duration {
	base := t.Config.RetryBaseDelay
	if base <= 0 {
		base = time.Second
	}

	capDelay := t.Config.RetryMaxDelay
	if capDelay <= 0 {
		capDelay = 30 * time.Second
	}

	backoff := float64(base) * math.Pow(2, float64(attempt))
	jitter := rand.Float64()*0.5 + 0.75
	sleep := time.Duration(backoff * jitter)
	if sleep > capDelay {
		return capDelay
	}
	return sleep
}

func filterValidFiles(files []aria2.File) []aria2.File {
	var valid []aria2.File
	for _, file := range files {
		if file.Selected == "true" && filepath.Ext(file.Path) != ".torrent" {
			valid = append(valid, file)
		}
	}
	return valid
}
