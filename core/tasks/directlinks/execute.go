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
	"time"

	"github.com/charmbracelet/log"
	"github.com/rs/xid"
	"golang.org/x/sync/errgroup"

	"github.com/merisssas/bot/common/utils/dlutil"
	"github.com/merisssas/bot/common/utils/ioutil"
	"github.com/merisssas/bot/config"
	"github.com/merisssas/bot/pkg/enums/ctxkey"
)

// Execute orchestrates the high-performance download pipeline.
func (t *Task) Execute(ctx context.Context) error {
	logger := log.FromContext(ctx)
	start := time.Now()

	logger.Infof("🚀 Starting Task Execution: %s | Files: %d", t.ID, len(t.files))

	if t.Progress != nil {
		t.Progress.OnStart(ctx, t)
	}

	// PHASE 1: Discovery & Intelligence (Probe)
	// We probe all files first to calculate total size and validate links.
	// This is crucial for a meaningful progress bar.
	logger.Debug("Phase 1: Probing links for metadata...")
	validFiles, err := t.runProbePhase(ctx)
	if err != nil {
		t.finish(ctx, err)
		return err
	}

	if len(validFiles) == 0 {
		err := errors.New("no valid files found to download")
		t.finish(ctx, err)
		return err
	}

	// Update Task State
	t.files = validFiles
	t.totalFiles = int64(len(validFiles))

	logger.Infof("✅ Probe Complete. Valid Files: %d | Total Size: %s", t.totalFiles, dlutil.FormatSize(t.totalBytes))

	// PHASE 2: Heavy Lifting (Download)
	logger.Debug("Phase 2: Starting download pipeline...")
	downloadErr := t.runDownloadPhase(ctx)

	// Finalize
	duration := time.Since(start)
	if downloadErr != nil {
		logger.Errorf("❌ Task Failed after %s: %v", duration, downloadErr)
	} else {
		logger.Infof("✅ Task Completed Successfully in %s", duration)
		t.removeState(ctx)
	}

	t.finish(ctx, downloadErr)
	return downloadErr
}

// runProbePhase executes concurrent HEAD requests to gather metadata.
func (t *Task) runProbePhase(ctx context.Context) ([]*File, error) {
	var (
		mu          sync.Mutex
		validFiles  = make([]*File, 0, len(t.files))
		probeErrors []error
		totalBytes  atomic.Int64
	)

	// Use ErrGroup for managed concurrency
	eg, gctx := errgroup.WithContext(ctx)

	// Limit concurrency to prevent DNS floods or IP bans
	limit := config.C().Workers
	if limit < 1 {
		limit = 4
	}
	eg.SetLimit(limit)

	for _, file := range t.files {
		file := file // Capture loop variable
		eg.Go(func() error {
			// Check context before starting work
			if gctx.Err() != nil {
				return gctx.Err()
			}

			// Execute Probe with Retry
			if err := t.probeFile(gctx, file); err != nil {
				mu.Lock()
				t.failed[file.URL] = err
				probeErrors = append(probeErrors, fmt.Errorf("%s: %w", file.URL, err))
				mu.Unlock()
				// We do NOT return error here, because we want to continue probing other files
				// unless it's a context cancellation.
				return nil
			}

			// Post-Probe Processing
			if file.Size > 0 {
				totalBytes.Add(file.Size)
			}

			// Sanitize Name
			file.Name = sanitizeFilename(file.Name)
			if file.Name == "" {
				file.Name = fmt.Sprintf("direct_%s", xid.New().String())
			}

			mu.Lock()
			validFiles = append(validFiles, file)
			mu.Unlock()
			return nil
		})
	}

	// Wait for all probes to finish
	if err := eg.Wait(); err != nil {
		return nil, err
	}

	t.totalBytes = totalBytes.Load()

	// Logic: If NO files are valid, fail the task.
	// If SOME are valid, proceed with partial success.
	if len(validFiles) == 0 && len(probeErrors) > 0 {
		return nil, errors.Join(probeErrors...)
	}

	return validFiles, nil
}

// runDownloadPhase orchestrates the actual file transfers.
func (t *Task) runDownloadPhase(ctx context.Context) error {
	logger := log.FromContext(ctx)
	eg, gctx := errgroup.WithContext(ctx)

	// Dynamic Worker Limiter
	// For downloads, we might want fewer concurrent files if files are huge,
	// or more if they are small. For now, we stick to config.
	limit := config.C().Workers
	if limit < 1 {
		limit = 2 // Default safer limit for heavy downloads
	}
	eg.SetLimit(limit)

	var errList []error
	var errMu sync.Mutex

	for _, file := range t.files {
		file := file
		eg.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}

			// State Locking: Prevent duplicate processing if logic gets complex later
			t.processingMu.Lock()
			if _, exists := t.processing[file.URL]; exists {
				t.processingMu.Unlock()
				return nil
			}
			t.processing[file.URL] = file
			t.processingMu.Unlock()

			defer func() {
				t.processingMu.Lock()
				delete(t.processing, file.URL)
				t.processingMu.Unlock()
			}()

			// Process Link
			logger.Debugf("⬇️ Processing: %s", file.Name)
			err := t.processLink(gctx, file)

			if err != nil {
				// Handle cancellation gracefully
				if errors.Is(err, context.Canceled) {
					return err
				}

				logger.Errorf("Failed %s: %v", file.Name, err)

				errMu.Lock()
				errList = append(errList, fmt.Errorf("failed %s: %w", file.Name, err))
				errMu.Unlock()

				// Optional: Fail whole task on single error?
				// Usually for direct links list, we want "Best Effort".
				// Return nil to keep other downloads going.
				return nil
			}

			t.downloaded.Add(1)
			return nil
		})
	}

	// Wait for downloads
	if err := eg.Wait(); err != nil {
		return err
	}

	// If there were individual file errors, summarize them
	if len(errList) > 0 {
		return errors.Join(errList...)
	}

	return nil
}

// processLink handles the lifecycle of a single file download.
func (t *Task) processLink(ctx context.Context, file *File) error {
	logger := log.FromContext(ctx)

	return retryWithPolicy(ctx, uint(config.C().Retry), 2*time.Second, func() error {
		// Context Injection for Storage Drivers (e.g. S3 needs ContentLength)
		if file.Size > 0 {
			ctx = context.WithValue(ctx, ctxkey.ContentLength, file.Size)
		}

		// STRATEGY 1: Direct Stream (Memory Efficient)
		if t.stream {
			return t.streamDownload(ctx, file)
		}

		// STRATEGY 2: Cache-First (Resumable & Robust)
		// This uses the "Bulletproof" downloadToTemp logic from download.go
		cacheFile, err := t.downloadToTemp(ctx, file)
		if err != nil {
			// If context canceled, stop immediately
			if errors.Is(err, context.Canceled) {
				return unrecoverable(err)
			}
			return err // Retryable error
		}

		// Ensure cleanup of temp file
		defer func() {
			if closeErr := cacheFile.CloseAndRemove(); closeErr != nil {
				logger.Warnf("Cleanup warning: %v", closeErr)
			}
		}()

		// Reset pointer before upload
		if _, err := cacheFile.Seek(0, 0); err != nil {
			return unrecoverable(fmt.Errorf("disk io error: %w", err))
		}

		// Upload to Final Storage
		destPath := filepath.Join(t.StorPath, file.Name)
		if err := t.Storage.Save(ctx, cacheFile, destPath); err != nil {
			return err // Retry upload failures
		}

		return nil
	})
}

// streamDownload handles direct piping without temp file (good for small RAM, stable net)
func (t *Task) streamDownload(ctx context.Context, file *File) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, file.URL, nil)
	if err != nil {
		return unrecoverable(err)
	}
	applyCustomHeaders(req, file.Headers)
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return unrecoverable(fmt.Errorf("fatal http status %d", resp.StatusCode))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("bad status %d", resp.StatusCode)
	}

	// Calculate checksums on the fly
	var r io.Reader = resp.Body

	// Progress Wrapper
	r = t.wrapProgressReader(ctx, r)

	// MD5 Wrapper
	md5Reader := newMD5TrackingReader(r)
	r = md5Reader // Use the tracker as the reader

	// Pipe to Storage
	destPath := filepath.Join(t.StorPath, file.Name)
	if err := t.Storage.Save(ctx, r, destPath); err != nil {
		return err
	}

	// Verify Integrity
	if file.ContentMD5 != "" {
		if err := verifyContentMD5(file.ContentMD5, md5Reader.Sum()); err != nil {
			return unrecoverable(fmt.Errorf("integrity check failed: %w", err))
		}
	}
	return nil
}

// probeFile implements smart discovery with fallback strategies.
func (t *Task) probeFile(ctx context.Context, file *File) error {
	logger := log.FromContext(ctx)

	return retryWithPolicy(ctx, uint(config.C().Retry), 0, func() error {
		// Try HEAD first (Fastest)
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, file.URL, nil)
		if err != nil {
			return unrecoverable(err)
		}
		applyCustomHeaders(req, file.Headers)
		req.Header.Set("Accept-Encoding", "identity")

		resp, err := t.client.Do(req)
		if err != nil {
			return err // Retry connection errors
		}
		defer resp.Body.Close()

		// Server doesn't like HEAD? Try Range GET (Smart Fallback)
		if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented {
			logger.Debugf("HEAD failed, switching to Range Probe: %s", file.URL)
			return t.probeFileRange(ctx, file)
		}

		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
			return unrecoverable(fmt.Errorf("link dead: %d", resp.StatusCode))
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("status %d", resp.StatusCode)
		}

		t.extractMetadata(resp, file)
		return nil
	})
}

func (t *Task) probeFileRange(ctx context.Context, file *File) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, file.URL, nil)
	if err != nil {
		return unrecoverable(err)
	}

	applyCustomHeaders(req, file.Headers)
	// Request only the first byte
	req.Header.Set("Range", "bytes=0-0")
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("range probe failed: %d", resp.StatusCode)
	}

	t.extractMetadata(resp, file)

	// Special handling for Range response size
	if resp.StatusCode == http.StatusPartialContent {
		file.Size = parseContentRangeSize(resp.Header.Get("Content-Range"))
		file.SupportsRange = true
	} else {
		// Server ignored range, sent full file (200 OK)
		// Usually means no range support
		file.SupportsRange = false
	}
	return nil
}

// extractMetadata centralizes header parsing logic
func (t *Task) extractMetadata(resp *http.Response, file *File) {
	// Size logic
	if file.Size <= 0 {
		file.Size = resp.ContentLength
	}

	// Filename logic
	if name := resp.Header.Get("Content-Disposition"); name != "" {
		filename := parseFilename(name)
		if filename != "" {
			file.Name = filename
		}
	}
	if file.Name == "" {
		file.Name = parseFilenameFromURL(file.URL)
	}

	// Capability logic
	if ranges := strings.ToLower(resp.Header.Get("Accept-Ranges")); strings.Contains(ranges, "bytes") {
		file.SupportsRange = true
	}

	// Integrity logic
	if file.ContentMD5 == "" {
		file.ContentMD5 = resp.Header.Get("Content-MD5")
	}
	if file.ETag == "" {
		file.ETag = resp.Header.Get("ETag")
	}
	if file.LastModified == "" {
		file.LastModified = resp.Header.Get("Last-Modified")
	}
}

func (t *Task) wrapProgressReader(ctx context.Context, body io.Reader) io.Reader {
	if t.Progress == nil {
		return body
	}
	// Use ioutil wrapper to update atomic counters
	progressWriter := ioutil.NewProgressWriter(io.Discard, func(n int) {
		t.downloadedBytes.Add(int64(n))
		t.Progress.OnProgress(ctx, t)
	})
	return io.TeeReader(body, progressWriter)
}

// finish helper to ensure progress executes Done
func (t *Task) finish(ctx context.Context, err error) {
	if t.Progress != nil {
		t.Progress.OnDone(ctx, t, err)
	}
}

// --- Utils ---

func parseContentRangeSize(value string) int64 {
	if value == "" {
		return 0
	}
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return 0
	}

	sizePart := strings.TrimSpace(parts[1])
	if sizePart == "*" {
		return 0 // Unknown size
	}

	size, err := strconv.ParseInt(sizePart, 10, 64)
	if err != nil || size <= 0 {
		return 0
	}
	return size
}
