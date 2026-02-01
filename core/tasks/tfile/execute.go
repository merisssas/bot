package tfile

import (
	"context"
	"fmt"
	"os"
	"path"

	"github.com/charmbracelet/log"
	"github.com/duke-git/lancet/v2/retry"
	"github.com/merisssas/bot/common/tdler"
	"github.com/merisssas/bot/common/utils/fsutil"
	"github.com/merisssas/bot/config"
	"github.com/merisssas/bot/pkg/enums/ctxkey"
)

func (t *Task) Execute(ctx context.Context) error {
	logger := log.FromContext(ctx).WithPrefix(fmt.Sprintf("file[%s]", t.File.Name()))
	info := &taskInfoOverride{
		task:        t,
		storagePath: t.Path,
	}
	if t.Progress != nil {
		t.Progress.OnStart(ctx, info)
	}
	if t.stream {
		return executeStream(ctx, info)
	}

	logger.Info("Starting file download")
	localFile, err := fsutil.CreateFile(t.localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	shouldCleanup := false
	defer func() {
		var cleanupErr error
		if shouldCleanup {
			cleanupErr = localFile.CloseAndRemove()
		} else {
			cleanupErr = localFile.Close()
		}
		if cleanupErr != nil {
			logger.Errorf("Failed to close local file: %v", cleanupErr)
		}
	}()
	wrAt := newWriterAt(ctx, localFile, t.Progress, info)

	defer func() {
		if t.Progress != nil {
			t.Progress.OnDone(ctx, info, err)
		}
	}()
	_, err = tdler.NewDownloader(t.File).Parallel(ctx, wrAt)
	if err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}
	logger.Infof("File downloaded successfully")
	if path.Ext(t.File.Name()) == "" {
		ext := fsutil.DetectFileExt(t.localPath)
		if ext != "" {
			info.storagePath = info.storagePath + ext
		}
	}
	var fileStat os.FileInfo
	fileStat, err = os.Stat(t.localPath)
	if err != nil {
		return fmt.Errorf("failed to get file stat: %w", err)
	}
	vctx := context.WithValue(ctx, ctxkey.ContentLength, fileStat.Size())
	err = retry.Retry(func() error {
		file, err := os.Open(t.localPath)
		if err != nil {
			return fmt.Errorf("failed to open cache file: %w", err)
		}
		defer file.Close()
		if err = t.Storage.Save(vctx, file, info.storagePath); err != nil {
			return fmt.Errorf("failed to save file: %w", err)
		}
		return nil
	}, retry.RetryTimes(uint(config.C().Retry)), retry.Context(vctx))
	if err != nil {
		return fmt.Errorf("failed to save file after retries: %w", err)
	}
	shouldCleanup = true
	return nil
}
