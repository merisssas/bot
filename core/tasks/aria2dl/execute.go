package aria2dl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/merisssas/Bot/config"
	"github.com/merisssas/Bot/pkg/aria2"
	"github.com/merisssas/Bot/pkg/enums/ctxkey"
	"golang.org/x/sync/errgroup"
)

const (
	maxConcurrentTransfers = 8
	uploadBufferSize       = 4 * 1024 * 1024
	maxRetriesDefault      = 5
)

// Execute implements core.Executable.
func (t *Task) Execute(ctx context.Context) error {
	logger := log.FromContext(ctx)
	logger.Infof("Starting aria2 download task %s (GID: %s)", t.ID, t.GID())

	if t.Progress != nil {
		t.Progress.OnStart(ctx, t)
	}

	// Wait for aria2 download to complete
	if err := t.waitForDownload(ctx); err != nil {
		// If context was canceled, also cancel the aria2 download
		if errors.Is(err, context.Canceled) {
			t.cancelAria2Download()
		}
		logger.Errorf("Aria2 download failed: %v", err)
		if t.Progress != nil {
			t.Progress.OnDone(ctx, t, err)
		}
		return err
	}

	// Transfer downloaded files to storage
	if err := t.transferFiles(ctx); err != nil {
		logger.Errorf("File transfer failed: %v", err)
		if t.Progress != nil {
			t.Progress.OnDone(ctx, t, err)
		}
		return err
	}

	logger.Infof("Aria2 task %s completed successfully", t.ID)
	if t.Progress != nil {
		t.Progress.OnDone(ctx, t, nil)
	}

	// Clean up aria2 download result
	if _, err := t.Aria2Client.RemoveDownloadResult(context.Background(), t.GID()); err != nil {
		logger.Warnf("Failed to remove aria2 download result: %v", err)
	}

	return nil
}

// waitForDownload waits for aria2 to complete the download
func (t *Task) waitForDownload(ctx context.Context) error {
	logger := log.FromContext(ctx)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			status, err := t.getStatus(ctx)
			if err != nil {
				logger.Warnf("Temporary status check failure: %v", err)
				continue
			}

			if t.Progress != nil {
				t.Progress.OnProgress(ctx, t, status)
			}
			t.UpdateStats(
				statusToInt64(status.CompletedLength),
				statusToInt64(status.TotalLength),
				statusToInt64(status.DownloadSpeed),
				statusToInt(status.Connections),
			)

			// Check if download is complete
			if status.IsDownloadComplete() {
				// Handle metadata downloads (torrent/magnet) that spawn follow-up downloads
				if len(status.FollowedBy) > 0 {
					logger.Infof("Switching from metadata GID %s to actual download GID: %s", t.GID(), status.FollowedBy[0])
					t.SetGID(status.FollowedBy[0])
					continue
				}
				logger.Infof("Download completed for GID %s", t.GID())
				return nil
			}

			// Check for errors
			if status.IsDownloadError() {
				t.SetError(fmt.Errorf("aria2 download error: %s (code: %s)", status.ErrorMessage, status.ErrorCode))
				return fmt.Errorf("aria2 download error: %s (code: %s)", status.ErrorMessage, status.ErrorCode)
			}

			if status.IsDownloadRemoved() {
				t.SetError(errors.New("aria2 download was removed"))
				return errors.New("aria2 download was removed")
			}
		}
	}
}

// getStatus retrieves the current status of the download
func (t *Task) getStatus(ctx context.Context) (*aria2.Status, error) {
	logger := log.FromContext(ctx)

	// Try active/waiting queue first
	status, err := t.Aria2Client.TellStatus(ctx, t.GID())
	if err == nil {
		return status, nil
	}

	// Check stopped queue
	logger.Debugf("Task not in active queue, checking stopped queue")
	stoppedTasks, stopErr := t.Aria2Client.TellStopped(ctx, -1, 100)
	if stopErr != nil {
		return nil, fmt.Errorf("failed to get aria2 status: %w", err)
	}

	for _, task := range stoppedTasks {
		if task.GID == t.GID() {
			logger.Debugf("Found task in stopped queue with status: %s", task.Status)
			return &task, nil
		}
	}

	return nil, fmt.Errorf("task GID %s not found: %w", t.GID(), err)
}

// transferFiles transfers downloaded files from aria2 to storage
func (t *Task) transferFiles(ctx context.Context) error {
	logger := log.FromContext(ctx)

	status, err := t.Aria2Client.TellStatus(ctx, t.GID())
	if err != nil {
		return fmt.Errorf("failed to get final status: %w", err)
	}

	if len(status.Files) == 0 {
		return errors.New("no files in aria2 download")
	}

	commonDir := filepath.Dir(status.Files[0].Path)
	if status.Dir != "" {
		commonDir = status.Dir
	}

	logger.Infof("Transferring %d file(s) to storage %s with up to %d concurrent workers", len(status.Files), t.Storage.Name(), maxConcurrentTransfers)

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(maxConcurrentTransfers)

	transferredCount := 0
	var mu sync.Mutex

	for _, file := range status.Files {
		if file.Selected != "true" {
			logger.Debugf("Skipping unselected file: %s", file.Path)
			continue
		}

		fileName := filepath.Base(file.Path)
		if filepath.Ext(fileName) == ".torrent" {
			logger.Debugf("Skipping torrent metadata file: %s", fileName)
			t.removeFileIfNeeded(ctx, file.Path)
			continue
		}

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

			mu.Lock()
			transferredCount++
			mu.Unlock()

			t.removeFileIfNeeded(groupCtx, file.Path)
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return err
	}

	if transferredCount == 0 {
		return errors.New("no files were transferred")
	}

	return nil
}

func (t *Task) transferFileWithRetry(ctx context.Context, srcPath, destRelPath string) error {
	logger := log.FromContext(ctx)
	maxRetries := t.Config.MaxRetries
	if maxRetries <= 0 {
		maxRetries = maxRetriesDefault
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := t.transferSingleFile(ctx, srcPath, destRelPath); err == nil {
			return nil
		} else if os.IsNotExist(err) {
			logger.Warnf("Downloaded file not found: %s", srcPath)
			return nil
		} else if attempt == maxRetries {
			t.SetError(err)
			return fmt.Errorf("max retries reached for %s: %w", destRelPath, err)
		} else {
			backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			retryCount := t.IncrementRetry()
			t.SetError(err)
			logger.Warnf("Transfer failed for %s (attempt %d/%d, retry=%d), retrying in %v: %v", destRelPath, attempt+1, maxRetries, retryCount, backoff, err)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
	}

	return nil
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

	reader := bufio.NewReaderSize(f, uploadBufferSize)
	ctx = context.WithValue(ctx, ctxkey.ContentLength, fileInfo.Size())

	destPath := filepath.Join(t.StorPath, destRelPath)
	logger.Infof("Transferring file %s to %s:%s (%s)", filepath.Base(srcPath), t.Storage.Name(), destPath, formatBytes(fileInfo.Size()))

	if err := t.Storage.Save(ctx, reader, destPath); err != nil {
		return err
	}

	logger.Infof("Successfully transferred file %s", filepath.Base(srcPath))
	return nil
}

// removeFileIfNeeded removes a file if RemoveAfterTransfer is enabled
func (t *Task) removeFileIfNeeded(ctx context.Context, filePath string) {
	if config.C().Aria2.KeepFile {
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

// cancelAria2Download cancels the aria2 download task
func (t *Task) cancelAria2Download() {
	logger := log.FromContext(t.ctx)
	logger.Infof("Canceling aria2 download GID: %s", t.GID())

	// Use a background context with timeout for cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try to force remove the download
	if _, err := t.Aria2Client.ForceRemove(ctx, t.GID()); err != nil {
		logger.Warnf("Failed to cancel aria2 download %s: %v", t.GID(), err)
	} else {
		logger.Infof("Successfully canceled aria2 download %s", t.GID())
	}

	// Also remove the download result to clean up
	if _, err := t.Aria2Client.RemoveDownloadResult(ctx, t.GID()); err != nil {
		logger.Debugf("Failed to remove download result for %s: %v", t.GID(), err)
	}
}

func formatBytes(size int64) string {
	if size == 0 {
		return "0 B"
	}

	sizes := []string{"B", "KB", "MB", "GB", "TB"}
	index := int(math.Floor(math.Log(float64(size)) / math.Log(1024)))
	value := float64(size) / math.Pow(1024, float64(index))

	return fmt.Sprintf("%.2f %s", value, sizes[index])
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
