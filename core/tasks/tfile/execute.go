package tfile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/merisssas/Bot/common/tdler"
	"github.com/merisssas/Bot/common/utils/fsutil"
	"github.com/merisssas/Bot/config"
	"github.com/merisssas/Bot/pkg/enums/ctxkey"
	tfilepkg "github.com/merisssas/Bot/pkg/tfile"
)

func (t *Task) Execute(ctx context.Context) (err error) {
	logger := log.FromContext(ctx).WithPrefix(fmt.Sprintf("file[%s]", t.File.Name()))
	if strings.TrimSpace(t.File.Name()) == "" {
		return errors.New("file name is empty")
	}

	resolvedPath, skip, err := resolveStoragePath(ctx, t.Storage, t.Path, config.C().TFile.OverwritePolicy)
	if err != nil {
		return err
	}
	t.Path = resolvedPath

	defer func() {
		if t.Progress != nil {
			t.Progress.OnDone(ctx, t, err)
		}
	}()
	if t.Progress != nil {
		t.Progress.OnStart(ctx, t)
	}
	if skip {
		logger.Infof("Skipping download because destination already exists: %s", t.Path)
		return nil
	}
	if config.C().TFile.DryRun {
		logger.Infof("Dry-run: %s (%d bytes) -> %s:%s", t.File.Name(), t.File.Size(), t.Storage.Name(), t.Path)
		return nil
	}
	if t.stream {
		return executeStream(ctx, t)
	}

	logger.Info("Starting file download")
	cachePath, err := downloadWithRetry(ctx, logger, t)
	if err != nil {
		return err
	}
	if path.Ext(t.File.Name()) == "" {
		ext := fsutil.DetectFileExt(cachePath)
		if ext != "" {
			t.Path = t.Path + ext
		}
	}
	fileStat, err := os.Stat(cachePath)
	if err != nil {
		return fmt.Errorf("failed to get file stat: %w", err)
	}
	if err := validateDownloadedSize(t.File.Size(), fileStat.Size()); err != nil {
		return err
	}
	vctx := context.WithValue(ctx, ctxkey.ContentLength, fileStat.Size())
	err = retryWithBackoff(vctx, logger, newRetryConfig(config.C()), func(attempt int) error {
		file, err := os.Open(cachePath)
		if err != nil {
			return fmt.Errorf("failed to open cache file: %w", err)
		}
		defer file.Close()
		if err := t.Storage.Save(vctx, file, t.Path); err != nil {
			return fmt.Errorf("failed to save file: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to save file after retries: %w", err)
	}
	if !config.C().NoCleanCache {
		if err := os.Remove(cachePath); err != nil {
			logger.Errorf("Failed to remove cache file: %v", err)
		}
	}
	logger.Infof("File downloaded successfully")
	return nil
}

func downloadWithRetry(ctx context.Context, logger *log.Logger, task *Task) (string, error) {
	cachePath := task.localPath
	expectedSize := task.File.Size()
	if ok, err := validateCacheFile(cachePath, expectedSize); err != nil {
		return "", err
	} else if ok {
		logger.Infof("Using cached file for %s", task.File.Name())
		return cachePath, nil
	}

	partPath := cachePath + ".part"
	err := retryWithBackoff(ctx, logger, newRetryConfig(config.C()), func(attempt int) error {
		localFile, err := fsutil.CreateFile(partPath)
		if err != nil {
			return fmt.Errorf("failed to create cache file: %w", err)
		}

		wrAt := newWriterAt(ctx, localFile, task.Progress, task)
		_, err = tdler.NewDownloader(task.File).Parallel(ctx, wrAt)
		if err != nil {
			if isLocationNotFound(err) {
				if refreshErr := tryRefreshFile(ctx, logger, task.File); refreshErr != nil {
					logger.Warnf("Failed to refresh file location: %v", refreshErr)
				}
			}
			if removeErr := localFile.CloseAndRemove(); removeErr != nil {
				logger.Errorf("Failed to remove cache file: %v", removeErr)
			}
			return fmt.Errorf("failed to download file: %w", err)
		}
		if err := localFile.Close(); err != nil {
			if removeErr := localFile.Remove(); removeErr != nil {
				logger.Errorf("Failed to remove cache file: %v", removeErr)
			}
			return fmt.Errorf("failed to close cache file: %w", err)
		}
		if err := os.Remove(cachePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove stale cache: %w", err)
		}
		if err := os.Rename(partPath, cachePath); err != nil {
			return fmt.Errorf("failed to finalize cache file: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return cachePath, nil
}

func tryRefreshFile(ctx context.Context, logger *log.Logger, file tfilepkg.TGFile) error {
	refreshable, ok := file.(tfilepkg.Refreshable)
	if !ok {
		return nil
	}
	if err := refreshable.Refresh(ctx); err != nil {
		return err
	}
	if logger != nil {
		logger.Warn("Refreshed file reference for retry")
	}
	return nil
}

func validateCacheFile(cachePath string, expectedSize int64) (bool, error) {
	stat, err := os.Stat(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to stat cache file: %w", err)
	}
	if stat.Size() == 0 {
		if err := os.Remove(cachePath); err != nil {
			return false, fmt.Errorf("failed to remove empty cache file: %w", err)
		}
		return false, nil
	}
	if expectedSize > 0 && stat.Size() != expectedSize {
		if err := os.Remove(cachePath); err != nil {
			return false, fmt.Errorf("failed to remove stale cache file: %w", err)
		}
		return false, nil
	}
	return true, nil
}

func validateDownloadedSize(expected, actual int64) error {
	if expected == 0 || actual == 0 {
		return nil
	}
	if expected != actual {
		return fmt.Errorf("downloaded file size mismatch: expected %d, got %d", expected, actual)
	}
	return nil
}

func newRetryConfig(cfg config.Config) retryConfig {
	return retryConfig{
		maxRetries: cfg.TFile.MaxRetries,
		baseDelay:  cfg.TFile.RetryBaseDelay,
		maxDelay:   cfg.TFile.RetryMaxDelay,
		jitter:     cfg.TFile.RetryJitter,
	}
}
