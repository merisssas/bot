package tfile

import (
	"context"
	"io"
	"sync"
)

type ProgressWriterAt struct {
	ctx        context.Context
	wrAt       io.WriterAt
	progress   ProgressTracker
	mu         sync.Mutex
	downloaded int64
	total      int64
	info       TaskInfo
}

func (w *ProgressWriterAt) WriteAt(p []byte, off int64) (int, error) {
	at, err := w.wrAt.WriteAt(p, off)
	if err != nil {
		return 0, err
	}
	if w.progress != nil {
		w.mu.Lock()
		end := off + int64(at)
		if end > w.downloaded {
			w.downloaded = end
		}
		downloaded := w.downloaded
		w.mu.Unlock()
		w.progress.OnProgress(w.ctx, w.info, downloaded, w.total)
	}
	return at, nil
}

func newWriterAt(
	ctx context.Context,
	wrAt io.WriterAt,
	progress ProgressTracker,
	taskInfo TaskInfo,
) *ProgressWriterAt {
	return &ProgressWriterAt{
		ctx:      ctx,
		progress: progress,
		total:    taskInfo.FileSize(),
		wrAt:     wrAt,
		info:     taskInfo,
	}
}

type ProgressWriter struct {
	ctx        context.Context
	wrAt       io.Writer
	progress   ProgressTracker
	mu         sync.Mutex
	downloaded int64
	total      int64
	info       TaskInfo
}

func (w *ProgressWriter) Write(p []byte) (int, error) {
	at, err := w.wrAt.Write(p)
	if err != nil {
		return 0, err
	}
	if w.progress != nil {
		w.mu.Lock()
		w.downloaded += int64(at)
		downloaded := w.downloaded
		w.mu.Unlock()
		w.progress.OnProgress(w.ctx, w.info, downloaded, w.total)
	}
	return at, nil
}

func newWriter(
	ctx context.Context,
	wr io.Writer,
	progress ProgressTracker,
	taskInfo TaskInfo,
) *ProgressWriter {
	return &ProgressWriter{
		ctx:      ctx,
		progress: progress,
		total:    taskInfo.FileSize(),
		wrAt:     wr,
		info:     taskInfo,
	}
}
