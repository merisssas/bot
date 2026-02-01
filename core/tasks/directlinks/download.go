package directlinks

import (
	"context"
	"crypto/md5"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"golang.org/x/sync/errgroup"

	"github.com/merisssas/bot/common/utils/fsutil"
	"github.com/merisssas/bot/common/utils/ioutil"
	"github.com/merisssas/bot/config"
)

const (
	// Enterprise Grade Tuning
	defaultSegmentSize        = int64(16 * 1024 * 1024)   // 16MB chunks for better throughput
	maxParallelSegments       = 64                        // Support for Gigabit connections
	minSegmentSize            = int64(1 * 1024 * 1024)    // Don't split smaller than 1MB
	dynamicSegmentSpeedTarget = float64(12 * 1024 * 1024) // Target 12MB/s per thread
	bufferSize                = 32 * 1024                 // 32KB buffer for CopyBuffer
)

// Global Buffer Pool to reduce GC pressure to almost zero during downloads
var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, bufferSize)
	},
}

type md5TrackingReader struct {
	reader io.Reader
	hash   hash.Hash
}

func newMD5TrackingReader(reader io.Reader) *md5TrackingReader {
	return &md5TrackingReader{
		reader: reader,
		hash:   md5.New(),
	}
}

func (m *md5TrackingReader) Read(p []byte) (int, error) {
	n, err := m.reader.Read(p)
	if n > 0 {
		_, _ = m.hash.Write(p[:n])
	}
	return n, err
}

func (m *md5TrackingReader) Sum() []byte {
	if m == nil {
		return nil
	}
	return m.hash.Sum(nil)
}

func verifyContentMD5(expected string, actual []byte) error {
	expected = strings.Trim(strings.TrimSpace(expected), "\"")
	if expected == "" {
		return nil
	}
	expectedBytes, err := decodeContentMD5(expected)
	if err != nil {
		return fmt.Errorf("bad header md5: %w", err)
	}
	if len(actual) == 0 {
		return errors.New("empty actual md5")
	}
	if subtle.ConstantTimeCompare(expectedBytes, actual) != 1 {
		return fmt.Errorf("mismatch: expected %x, got %x", expectedBytes, actual)
	}
	return nil
}

func decodeContentMD5(expected string) ([]byte, error) {
	if isHexMD5(expected) {
		return hex.DecodeString(expected)
	}
	return base64.StdEncoding.DecodeString(expected)
}

func isHexMD5(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

// downloadToTemp is the entry point for the "Bulletproof" download process
func (t *Task) downloadToTemp(ctx context.Context, file *File) (*fsutil.File, error) {
	logger := log.FromContext(ctx)

	tempName := sanitizeFilename(file.Name)
	if tempName == "" {
		tempName = fmt.Sprintf("file_%s", t.ID)
	}

	fullPath := filepath.Join(config.C().Temp.BasePath, fmt.Sprintf("direct_%s_%s", t.ID, tempName))

	if err := fsutil.MkdirAll(filepath.Dir(fullPath)); err != nil {
		return nil, fmt.Errorf("fs error: %w", err)
	}

	cacheFile, err := fsutil.CreateFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp artifact: %w", err)
	}

	var downloadErr error
	strategy := "Sequential"

	if file.Size > 0 && file.SupportsRange && !file.DisableParallel {
		strategy = "Parallel (Smart)"
		downloadErr = t.downloadWithSmartRanges(ctx, file, cacheFile)

		if downloadErr != nil && !errors.Is(downloadErr, context.Canceled) {
			logger.Warnf("Parallel strategy failed: %v. Fallback to Sequential.", downloadErr)

			if resetErr := resetCacheFile(cacheFile); resetErr != nil {
				_ = cacheFile.CloseAndRemove()
				return nil, fmt.Errorf("critical fs failure during reset: %w", resetErr)
			}
			strategy = "Sequential (Fallback)"
			downloadErr = t.downloadSequential(ctx, file, cacheFile)
		}
	} else {
		downloadErr = t.downloadSequential(ctx, file, cacheFile)
	}

	if downloadErr != nil {
		_ = cacheFile.CloseAndRemove()
		return nil, fmt.Errorf("[%s] download failed: %w", strategy, downloadErr)
	}

	return cacheFile, nil
}

// downloadWithSmartRanges implements the "Race Start" algorithm.
// It downloads the first chunk to measure speed, then scales workers dynamically.
func (t *Task) downloadWithSmartRanges(ctx context.Context, file *File, cacheFile *fsutil.File) error {
	logger := log.FromContext(ctx)

	maxWorkers := segmentLimitFromWorkers(config.C().Workers)
	if maxWorkers <= 1 {
		return t.downloadSequential(ctx, file, cacheFile)
	}

	estimatedSegSize := segmentSizeForFile(file.Size, maxWorkers)
	firstSegmentSize := minInt64(estimatedSegSize, defaultSegmentSize)

	if file.Size <= firstSegmentSize*2 {
		return t.downloadSequential(ctx, file, cacheFile)
	}

	start := time.Now()
	if err := t.downloadRangeSegment(ctx, file, cacheFile, 0, firstSegmentSize); err != nil {
		return err
	}

	duration := time.Since(start)
	if duration == 0 {
		duration = 1
	}

	bps := float64(firstSegmentSize) / duration.Seconds()

	adjustedLimit := dynamicSegmentLimit(bps, maxWorkers)
	finalSegmentSize := segmentSizeForFile(file.Size, adjustedLimit)

	logger.Debugf("🚀 Smart Download: Speed=%.2f MB/s | Workers=%d | ChunkSize=%s",
		bps/1024/1024, adjustedLimit, formatSize(finalSegmentSize))

	return t.downloadRemainingSegments(ctx, file, cacheFile, firstSegmentSize, finalSegmentSize, adjustedLimit)
}

func (t *Task) downloadRemainingSegments(
	ctx context.Context,
	file *File,
	cacheFile *fsutil.File,
	startOffset int64,
	segmentSize int64,
	segmentLimit int,
) error {
	if startOffset >= file.Size {
		return nil
	}

	eg, gctx := errgroup.WithContext(ctx)
	eg.SetLimit(segmentLimit)

	for start := startOffset; start < file.Size; start += segmentSize {
		segmentStart := start
		segmentEnd := minInt64(segmentStart+segmentSize-1, file.Size-1)
		segmentLength := segmentEnd - segmentStart + 1

		sStart, sLen := segmentStart, segmentLength

		eg.Go(func() error {
			select {
			case <-gctx.Done():
				return gctx.Err()
			default:
			}
			return t.downloadRangeSegment(gctx, file, cacheFile, sStart, sLen)
		})
	}

	return eg.Wait()
}

// downloadRangeSegment handles a single chunk with robust retry and buffer pooling
func (t *Task) downloadRangeSegment(
	ctx context.Context,
	file *File,
	cacheFile *fsutil.File,
	segmentStart int64,
	segmentLength int64,
) error {
	if segmentLength <= 0 {
		return nil
	}
	segmentEnd := segmentStart + segmentLength - 1

	return retryWithPolicy(ctx, uint(config.C().Retry), 1*time.Second, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, file.URL, nil)
		if err != nil {
			return unrecoverable(fmt.Errorf("bad request: %w", err))
		}

		req.Header.Set("Accept-Encoding", "identity")
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", segmentStart, segmentEnd))

		if file.ETag != "" {
			req.Header.Set("If-Range", file.ETag)
		} else if file.LastModified != "" {
			req.Header.Set("If-Range", file.LastModified)
		}

		resp, err := t.client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
			return unrecoverable(fmt.Errorf("fatal status %d", resp.StatusCode))
		}
		if resp.StatusCode != http.StatusPartialContent {
			return unrecoverable(fmt.Errorf("server returned %d instead of 206", resp.StatusCode))
		}

		sectionWriter := newSectionWriter(cacheFile, segmentStart, segmentLength)

		progressWriter := ioutil.NewProgressWriter(sectionWriter, func(n int) {
			t.downloadedBytes.Add(int64(n))
			if t.Progress != nil {
				t.Progress.OnProgress(ctx, t)
			}
		})

		buf := bufferPool.Get().([]byte)
		defer bufferPool.Put(buf)

		if _, err := io.CopyBuffer(progressWriter, resp.Body, buf); err != nil {
			return err
		}
		return nil
	})
}

// downloadSequential handles standard downloads (or fallback)
func (t *Task) downloadSequential(ctx context.Context, file *File, cacheFile *fsutil.File) error {
	if file.SupportsRange && file.Size > 0 {
		return t.downloadSequentialWithResume(ctx, file, cacheFile)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, file.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}

	if file.ContentMD5 == "" {
		file.ContentMD5 = resp.Header.Get("Content-MD5")
	}

	var writer io.Writer = cacheFile
	var hasher hash.Hash

	if file.ContentMD5 != "" {
		hasher = md5.New()
		writer = io.MultiWriter(cacheFile, hasher)
	}

	writer = ioutil.NewProgressWriter(writer, func(n int) {
		t.downloadedBytes.Add(int64(n))
		if t.Progress != nil {
			t.Progress.OnProgress(ctx, t)
		}
	})

	buf := bufferPool.Get().([]byte)
	defer bufferPool.Put(buf)

	if _, err := io.CopyBuffer(writer, resp.Body, buf); err != nil {
		return err
	}

	if file.ContentMD5 != "" && hasher != nil {
		if err := verifyContentMD5(file.ContentMD5, hasher.Sum(nil)); err != nil {
			return err
		}
	}
	return nil
}

// downloadSequentialWithResume handles resume logic for single-thread
func (t *Task) downloadSequentialWithResume(ctx context.Context, file *File, cacheFile *fsutil.File) error {
	offset := int64(0)

	info, err := cacheFile.Stat()
	if err == nil {
		offset = info.Size()
	}
	if offset == file.Size {
		return nil
	}
	if offset > file.Size {
		cacheFile.Truncate(0)
		offset = 0
	}

	for offset < file.Size {
		err := retryWithPolicy(ctx, uint(config.C().Retry), 0, func() error {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, file.URL, nil)
			if err != nil {
				return err
			}

			req.Header.Set("Accept-Encoding", "identity")
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
			if file.ETag != "" {
				req.Header.Set("If-Range", file.ETag)
			} else if file.LastModified != "" {
				req.Header.Set("If-Range", file.LastModified)
			}

			resp, err := t.client.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			if offset == 0 && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
				return unrecoverable(fmt.Errorf("status %d", resp.StatusCode))
			}
			if resp.StatusCode != http.StatusPartialContent && offset > 0 {
				return unrecoverable(fmt.Errorf("server refused resume with %d", resp.StatusCode))
			}

			writer := newSectionWriter(cacheFile, offset, file.Size-offset)

			pw := ioutil.NewProgressWriter(writer, func(n int) {
				t.downloadedBytes.Add(int64(n))
				if t.Progress != nil {
					t.Progress.OnProgress(ctx, t)
				}
			})

			buf := bufferPool.Get().([]byte)
			defer bufferPool.Put(buf)

			copied, err := io.CopyBuffer(pw, resp.Body, buf)
			offset += copied
			return err
		})

		if err != nil {
			return err
		}
	}
	return nil
}

// --- Utils & Helpers ---

func resetCacheFile(cacheFile *fsutil.File) error {
	if err := cacheFile.Truncate(0); err != nil {
		return err
	}
	_, err := cacheFile.Seek(0, io.SeekStart)
	return err
}

func segmentLimitFromWorkers(workers int) int {
	if workers < 1 {
		return 4
	}
	limit := workers * 2
	if limit > maxParallelSegments {
		return maxParallelSegments
	}
	return limit
}

func segmentSizeForFile(size int64, limit int) int64 {
	if size <= 0 || limit <= 0 {
		return defaultSegmentSize
	}

	partSize := size / int64(limit)
	if partSize < minSegmentSize {
		return minSegmentSize
	}
	return partSize
}

func dynamicSegmentLimit(speedBps float64, maxSegments int) int {
	if speedBps <= 0 {
		return 1
	}

	if speedBps > dynamicSegmentSpeedTarget*4 {
		return max(4, maxSegments/2)
	}
	return maxSegments
}

func formatSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// newSectionWriter (Thread-safe writer for parallel chunks)
type sectionWriter struct {
	writerAt io.WriterAt
	offset   int64
	limit    int64
	written  int64
	mu       sync.Mutex
}

func newSectionWriter(writerAt io.WriterAt, offset, limit int64) io.Writer {
	return &sectionWriter{
		writerAt: writerAt,
		offset:   offset,
		limit:    limit,
	}
}

func (sw *sectionWriter) Write(p []byte) (int, error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	if sw.written >= sw.limit {
		return 0, io.ErrShortWrite
	}

	remaining := sw.limit - sw.written
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}

	n, err := sw.writerAt.WriteAt(p, sw.offset+sw.written)
	sw.written += int64(n)
	return n, err
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
