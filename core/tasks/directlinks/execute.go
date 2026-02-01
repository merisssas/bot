package directlinks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/log"
	"github.com/duke-git/lancet/v2/retry"
	"github.com/merisssas/Bot/common/utils/fsutil"
	"github.com/merisssas/Bot/config"
	"github.com/merisssas/Bot/pkg/enums/ctxkey"
	"golang.org/x/sync/errgroup"
)

// Global buffer pool to reduce GC pressure during high-speed downloads.
var bufPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 32*1024)
		return &buf
	},
}

const (
	minFileSizeForMultipart = 5 * 1024 * 1024
	defaultChunkCount       = 16
	minChunkSize            = 1 * 1024 * 1024
)

func (t *Task) Execute(ctx context.Context) error {
	logger := log.FromContext(ctx)
	logger.Infof("Starting directlinks task %s", t.ID)
	if t.Progress != nil {
		t.Progress.OnStart(ctx, t)
	}

	eg, gctx := errgroup.WithContext(ctx)
	eg.SetLimit(config.C().Workers)
	fetchedTotalBytes := atomic.Int64{}
	for _, file := range t.files {
		file := file
		eg.Go(func() error {
			return t.analyzeFile(gctx, file, &fetchedTotalBytes)
		})
	}

	if err := eg.Wait(); err != nil {
		logger.Errorf("Error during file analysis: %v", err)
		if t.Progress != nil {
			t.Progress.OnDone(ctx, t, err)
		}
		return err
	}

	t.totalBytes = fetchedTotalBytes.Load()

	eg, gctx = errgroup.WithContext(ctx)
	eg.SetLimit(config.C().Workers)
	for _, file := range t.files {
		file := file
		eg.Go(func() error {
			t.processingMu.RLock()
			_, ok := t.processing[file.URL]
			t.processingMu.RUnlock()
			if ok {
				return fmt.Errorf("file %s is already being processed", file.URL)
			}

			t.processingMu.Lock()
			t.processing[file.URL] = file
			t.processingMu.Unlock()
			defer func() {
				t.processingMu.Lock()
				delete(t.processing, file.URL)
				t.processingMu.Unlock()
			}()

			canParallel := file.Size >= minFileSizeForMultipart && file.IsResumable
			var err error
			if canParallel {
				err = t.processLinkMultipart(gctx, file)
				if err != nil {
					logger.Warnf("Multipart failed for %s, falling back to single stream: %v", file.Name, err)
					err = t.processLinkSingle(gctx, file)
				}
			} else {
				err = t.processLinkSingle(gctx, file)
			}

			t.downloaded.Add(1)
			if errors.Is(err, context.Canceled) {
				logger.Debug("Link processing canceled")
				return err
			}
			if err != nil {
				t.RecordFailure(file.URL, err)
				logger.Errorf("Error processing link %s: %v", file.URL, err)
				return fmt.Errorf("failed to process link %s: %w", file.URL, err)
			}
			return nil
		})
	}

	err := eg.Wait()
	if err != nil {
		logger.Errorf("Error during directlinks task execution: %v", err)
	} else {
		logger.Infof("Directlinks task %s completed successfully", t.ID)
	}
	if t.Progress != nil {
		t.Progress.OnDone(ctx, t, err)
	}
	return err
}

func (t *Task) analyzeFile(ctx context.Context, file *File, totalBytes *atomic.Int64) error {
	retryTimes := uint(config.C().Retry)
	if retryTimes == 0 {
		retryTimes = 1
	}

	return retry.Retry(func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, file.URL, nil)
		if err != nil {
			return fmt.Errorf("failed to create HEAD request for %s: %w", file.URL, err)
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36")

		resp, err := t.client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to HEAD %s: %w", file.URL, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusForbidden {
				return t.analyzeFileFallback(ctx, file, totalBytes)
			}
			return fmt.Errorf("HEAD %s returned status %d", file.URL, resp.StatusCode)
		}

		if resp.ContentLength > 0 {
			totalBytes.Add(resp.ContentLength)
			file.Size = resp.ContentLength
		}

		file.ContentType = resp.Header.Get("Content-Type")
		if strings.EqualFold(resp.Header.Get("Accept-Ranges"), "bytes") && resp.ContentLength > 0 {
			file.IsResumable = true
		}

		file.Name = ParseFilename(resp.Header.Get("Content-Disposition"), file.URL, file.ContentType)
		if file.Name == "" {
			return fmt.Errorf("failed to determine filename for %s", file.URL)
		}

		return nil
	}, retry.RetryTimes(retryTimes), retry.Context(ctx))
}

func (t *Task) analyzeFileFallback(ctx context.Context, file *File, totalBytes *atomic.Int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, file.URL, nil)
	if err != nil {
		return fmt.Errorf("failed to create range GET request for %s: %w", file.URL, err)
	}
	req.Header.Set("Range", "bytes=0-0")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to range GET %s: %w", file.URL, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusPartialContent:
		file.IsResumable = true
		if total, ok := parseContentRangeTotal(resp.Header.Get("Content-Range")); ok {
			file.Size = total
			totalBytes.Add(total)
		}
	case http.StatusOK:
		if resp.ContentLength > 0 {
			file.Size = resp.ContentLength
			totalBytes.Add(resp.ContentLength)
		}
	default:
		return fmt.Errorf("range GET %s returned status %d", file.URL, resp.StatusCode)
	}

	file.ContentType = resp.Header.Get("Content-Type")
	file.Name = ParseFilename(resp.Header.Get("Content-Disposition"), file.URL, file.ContentType)
	if file.Name == "" {
		return fmt.Errorf("failed to determine filename for %s", file.URL)
	}

	return nil
}

func (t *Task) processLinkMultipart(ctx context.Context, file *File) error {
	logger := log.FromContext(ctx)
	logger.Infof("Accelerating download with multipart connections: %s", file.Name)
	if file.Size <= 0 {
		return fmt.Errorf("multipart requires known file size for %s", file.URL)
	}

	tempPath := filepath.Join(config.C().Temp.BasePath, fmt.Sprintf("direct_%s_%s", t.ID, file.Name))
	cacheFile, err := fsutil.CreateFile(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		if err := cacheFile.CloseAndRemove(); err != nil {
			logger.Errorf("Failed to close/remove cache file: %v", err)
		}
	}()

	if err := cacheFile.Truncate(file.Size); err != nil {
		return fmt.Errorf("failed to pre-allocate disk space: %w", err)
	}

	partSize := file.Size / int64(defaultChunkCount)
	if partSize < minChunkSize {
		partSize = minChunkSize
	}
	numParts := (file.Size + partSize - 1) / partSize

	eg, pctx := errgroup.WithContext(ctx)
	eg.SetLimit(defaultChunkCount)

	for i := int64(0); i < numParts; i++ {
		start := i * partSize
		end := start + partSize - 1
		if end >= file.Size {
			end = file.Size - 1
		}

		chunkStart := start
		chunkEnd := end
		eg.Go(func() error {
			return retry.Retry(func() error {
				req, err := http.NewRequestWithContext(pctx, http.MethodGet, file.URL, nil)
				if err != nil {
					return fmt.Errorf("failed to create ranged GET for %s: %w", file.URL, err)
				}
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", chunkStart, chunkEnd))

				resp, err := t.client.Do(req)
				if err != nil {
					return fmt.Errorf("failed to GET range %s: %w", file.URL, err)
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusPartialContent {
					return fmt.Errorf("server rejected range request with status %d", resp.StatusCode)
				}

				bufPtr := bufPool.Get().(*[]byte)
				defer bufPool.Put(bufPtr)
				buf := *bufPtr

				currentOffset := chunkStart
				for {
					if err := pctx.Err(); err != nil {
						return err
					}
					nr, rErr := resp.Body.Read(buf)
					if nr > 0 {
						nw, wErr := cacheFile.WriteAt(buf[:nr], currentOffset)
						if wErr != nil {
							return wErr
						}
						if nw != nr {
							return io.ErrShortWrite
						}
						added := int64(nw)
						t.downloadedBytes.Add(added)
						currentOffset += added
						if t.Progress != nil {
							t.Progress.OnProgress(pctx, t)
						}
					}
					if rErr != nil {
						if errors.Is(rErr, io.EOF) {
							break
						}
						return rErr
					}
				}
				return nil
			}, retry.RetryTimes(5), retry.Context(pctx))
		})
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	if _, err := cacheFile.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to rewind cache file: %w", err)
	}

	if err := t.Storage.Save(ctx, cacheFile, filepath.Join(t.StorPath, file.Name)); err != nil {
		return err
	}

	return nil
}

func (t *Task) processLinkSingle(ctx context.Context, file *File) error {
	logger := log.FromContext(ctx)
	return retry.Retry(func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, file.URL, nil)
		if err != nil {
			return fmt.Errorf("failed to create GET request for %s: %w", file.URL, err)
		}
		resp, err := t.client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to GET %s: %w", file.URL, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("GET %s returned status %d", file.URL, resp.StatusCode)
		}

		ctx = context.WithValue(ctx, ctxkey.ContentLength, file.Size)
		if t.stream {
			return t.Storage.Save(ctx, resp.Body, filepath.Join(t.StorPath, file.Name))
		}

		cacheFile, err := fsutil.CreateFile(filepath.Join(config.C().Temp.BasePath,
			fmt.Sprintf("direct_%s_%s", t.ID, file.Name)))
		if err != nil {
			return fmt.Errorf("failed to create temp file: %w", err)
		}
		defer func() {
			if err := cacheFile.CloseAndRemove(); err != nil {
				logger.Errorf("Failed to close and remove cache file: %v", err)
			}
		}()

		bufPtr := bufPool.Get().(*[]byte)
		defer bufPool.Put(bufPtr)
		buf := *bufPtr

		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			nr, rErr := resp.Body.Read(buf)
			if nr > 0 {
				nw, wErr := cacheFile.Write(buf[:nr])
				if wErr != nil {
					return wErr
				}
				if nw != nr {
					return io.ErrShortWrite
				}
				t.downloadedBytes.Add(int64(nw))
				if t.Progress != nil {
					t.Progress.OnProgress(ctx, t)
				}
			}
			if rErr != nil {
				if errors.Is(rErr, io.EOF) {
					break
				}
				return rErr
			}
		}

		if _, err = cacheFile.Seek(0, 0); err != nil {
			return fmt.Errorf("failed to seek cache file for resource %s: %w", file.URL, err)
		}

		return t.Storage.Save(ctx, cacheFile, filepath.Join(t.StorPath, file.Name))
	}, retry.RetryTimes(uint(config.C().Retry)), retry.Context(ctx))
}

func parseContentRangeTotal(header string) (int64, bool) {
	if header == "" {
		return 0, false
	}
	parts := strings.Split(header, "/")
	if len(parts) != 2 {
		return 0, false
	}
	totalStr := strings.TrimSpace(parts[1])
	if totalStr == "*" || totalStr == "" {
		return 0, false
	}
	total, err := strconv.ParseInt(totalStr, 10, 64)
	if err != nil {
		return 0, false
	}
	return total, true
}
