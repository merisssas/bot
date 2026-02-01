package tfile

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/charmbracelet/log"
	"github.com/merisssas/bot/common/tdler"
	"golang.org/x/sync/errgroup"
)

func executeStream(ctx context.Context, info *taskInfoOverride) error {
	logger := log.FromContext(ctx).WithPrefix(fmt.Sprintf("file[%s]", info.FileName()))

	pr, pw := io.Pipe()
	bufferedWriter := bufio.NewWriterSize(pw, 2<<20)
	var closeOnce sync.Once
	closePipeWithError := func(err error) {
		closeOnce.Do(func() {
			if err == nil {
				if flushErr := bufferedWriter.Flush(); flushErr != nil {
					err = flushErr
				}
			}
			if err != nil {
				_ = pr.CloseWithError(err)
				_ = pw.CloseWithError(err)
				return
			}
			_ = pr.Close()
			_ = pw.Close()
		})
	}
	errg, uploadCtx := errgroup.WithContext(ctx)
	errg.Go(func() error {
		if err := info.task.Storage.Save(uploadCtx, pr, info.storagePath); err != nil {
			closePipeWithError(err)
			return err
		}
		closePipeWithError(nil)
		return nil
	})
	wr := newWriter(ctx, bufferedWriter, info.task.Progress, info)
	errg.Go(func() error {
		defer closePipeWithError(nil)
		logger.Info("Starting file download in stream mode")
		_, err := tdler.NewDownloader(info.task.File).Stream(uploadCtx, wr)
		if err != nil {
			logger.Errorf("Failed to download file: %v", err)
			closePipeWithError(err)
		}
		return err
	})
	var err error
	defer func() {
		if info.task.Progress != nil {
			info.task.Progress.OnDone(uploadCtx, info, err)
		}
	}()
	if err = errg.Wait(); err != nil {
		return err
	}
	logger.Info("File downloaded successfully in stream mode")
	return nil
}
