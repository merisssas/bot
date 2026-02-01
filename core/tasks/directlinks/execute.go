package directlinks

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/log"
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

func (t *Task) Execute(ctx context.Context) error {
	logger := log.FromContext(ctx)
	logger.Infof("Starting directlinks task %s", t.ID)
	if t.Progress != nil {
		t.Progress.OnStart(ctx, t)
	}
	defer t.closeLogger()

	if err := t.validateInputs(); err != nil {
		if t.Progress != nil {
			t.Progress.OnDone(ctx, t, err)
		}
		return err
	}

	if err := t.refreshTransportProxy(); err != nil {
		logger.Warnf("Failed to apply proxy configuration: %v", err)
	}

	analysisErr := t.analyzeAll(ctx)
	if analysisErr != nil {
		logger.Errorf("Error during file analysis: %v", analysisErr)
		if t.Progress != nil {
			t.Progress.OnDone(ctx, t, analysisErr)
		}
		return analysisErr
	}

	if config.C().Directlinks.DryRun {
		logger.Infof("Directlinks task %s dry-run completed", t.ID)
		if t.Progress != nil {
			t.Progress.OnDone(ctx, t, nil)
		}
		return nil
	}

	downloadErr := t.downloadAll(ctx)
	if downloadErr != nil {
		logger.Errorf("Error during directlinks task execution: %v", downloadErr)
	} else {
		logger.Infof("Directlinks task %s completed successfully", t.ID)
	}
	if t.Progress != nil {
		t.Progress.OnDone(ctx, t, downloadErr)
	}
	return downloadErr
}

func (t *Task) validateInputs() error {
	if t.Storage == nil {
		return wrapError(ErrKindValidation, "storage is required", errors.New("storage is nil"))
	}
	if len(t.files) == 0 {
		return wrapError(ErrKindValidation, "no directlinks provided", errors.New("links list is empty"))
	}

	for _, file := range t.files {
		if file == nil {
			return wrapError(ErrKindValidation, "file entry is nil", errors.New("nil file"))
		}
		if strings.TrimSpace(file.URL) == "" {
			return wrapError(ErrKindValidation, "file URL is empty", errors.New("empty URL"))
		}
		parsed, err := url.Parse(file.URL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return wrapError(ErrKindValidation, "invalid URL", fmt.Errorf("invalid URL: %s", file.URL))
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return wrapError(ErrKindValidation, "unsupported URL scheme", fmt.Errorf("unsupported scheme: %s", parsed.Scheme))
		}
	}
	if strings.TrimSpace(config.C().Directlinks.ExpectedChecksum) != "" &&
		strings.TrimSpace(config.C().Directlinks.ChecksumAlgorithm) == "" {
		return wrapError(ErrKindValidation, "checksum algorithm required", errors.New("expected checksum provided without algorithm"))
	}
	return nil
}

func (t *Task) analyzeAll(ctx context.Context) error {
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
		return err
	}
	t.totalBytes = fetchedTotalBytes.Load()

	for _, file := range t.files {
		resolved, err := t.resolveStoragePath(ctx, file.Name)
		if err != nil {
			return err
		}
		file.StorageFileName = resolved
	}

	return nil
}

func (t *Task) downloadAll(ctx context.Context) error {
	files := make([]*File, 0, len(t.files))
	files = append(files, t.files...)
	sort.SliceStable(files, func(i, j int) bool {
		return files[i].Priority > files[j].Priority
	})

	concurrency := config.C().Directlinks.MaxConcurrency
	if concurrency < 1 {
		concurrency = config.C().Threads
		if concurrency < 1 {
			concurrency = 2
		}
	}

	fileCh := make(chan *File)
	eg, gctx := errgroup.WithContext(ctx)
	for i := 0; i < concurrency; i++ {
		eg.Go(func() error {
			for file := range fileCh {
				if err := t.waitIfPaused(gctx); err != nil {
					return err
				}
				if err := t.processFile(gctx, file); err != nil {
					return err
				}
			}
			return nil
		})
	}

	go func() {
		defer close(fileCh)
		for _, file := range files {
			select {
			case <-gctx.Done():
				return
			case fileCh <- file:
			}
		}
	}()

	return eg.Wait()
}

func (t *Task) processFile(ctx context.Context, file *File) error {
	logger := log.FromContext(ctx)
	t.processingMu.RLock()
	_, ok := t.processing[file.URL]
	t.processingMu.RUnlock()
	if ok {
		return wrapError(ErrKindValidation, "duplicate processing detected", fmt.Errorf("file %s is already being processed", file.URL))
	}

	t.processingMu.Lock()
	t.processing[file.URL] = file
	t.processingMu.Unlock()
	defer func() {
		t.processingMu.Lock()
		delete(t.processing, file.URL)
		t.processingMu.Unlock()
	}()

	canParallel := file.Size >= t.segmentConfig.minFileSize && file.IsResumable
	var err error
	if canParallel {
		err = t.processLinkMultipart(ctx, file)
		if err != nil {
			logger.Warnf("Multipart failed for %s, falling back to single stream: %v", file.Name, err)
			err = t.processLinkSingle(ctx, file)
		}
	} else {
		err = t.processLinkSingle(ctx, file)
	}

	t.downloaded.Add(1)
	if errors.Is(err, context.Canceled) {
		logger.Debug("Link processing canceled")
		return err
	}
	if err != nil {
		t.RecordFailure(file.URL, err)
		logger.Errorf("Error processing link %s: %v", file.URL, err)
		return wrapError(ErrKindUnknown, fmt.Sprintf("failed to process link %s", file.URL), err)
	}
	return nil
}

func (t *Task) analyzeFile(ctx context.Context, file *File, totalBytes *atomic.Int64) error {
	return retryWithBackoff(ctx, t.retryPolicy, t.jitter, func() error {
		req, err := t.newRequest(ctx, http.MethodHead, file.URL, nil)
		if err != nil {
			return wrapError(ErrKindValidation, "failed to create HEAD request", err)
		}

		resp, err := t.client.Do(req)
		if err != nil {
			return wrapError(ErrKindNetwork, "failed to issue HEAD request", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusForbidden {
				return t.analyzeFileFallback(ctx, file, totalBytes)
			}
			return wrapError(ErrKindHTTP, "HEAD returned non-success", fmt.Errorf("HEAD %s returned status %d", file.URL, resp.StatusCode))
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
			return wrapError(ErrKindValidation, "failed to determine filename", fmt.Errorf("could not determine filename for %s", file.URL))
		}

		return nil
	})
}

func (t *Task) analyzeFileFallback(ctx context.Context, file *File, totalBytes *atomic.Int64) error {
	req, err := t.newRequest(ctx, http.MethodGet, file.URL, nil)
	if err != nil {
		return wrapError(ErrKindValidation, "failed to create range GET request", err)
	}
	req.Header.Set("Range", "bytes=0-0")

	resp, err := t.client.Do(req)
	if err != nil {
		return wrapError(ErrKindNetwork, "failed to range GET", err)
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
		return wrapError(ErrKindHTTP, "range GET returned non-success", fmt.Errorf("range GET %s returned status %d", file.URL, resp.StatusCode))
	}

	file.ContentType = resp.Header.Get("Content-Type")
	file.Name = ParseFilename(resp.Header.Get("Content-Disposition"), file.URL, file.ContentType)
	if file.Name == "" {
		return wrapError(ErrKindValidation, "failed to determine filename", fmt.Errorf("could not determine filename for %s", file.URL))
	}

	return nil
}

func (t *Task) processLinkMultipart(ctx context.Context, file *File) error {
	logger := log.FromContext(ctx)
	logger.Infof("Accelerating download with multipart connections: %s", file.Name)
	if file.Size <= 0 {
		return wrapError(ErrKindValidation, "multipart requires known file size", fmt.Errorf("multipart requires known file size for %s", file.URL))
	}

	tempPath := filepath.Join(config.C().Temp.BasePath, fmt.Sprintf("direct_%s_%s.part", t.ID, file.Name))
	metaPath := tempPath + ".meta"
	cacheFile, err := openResumeFile(tempPath, file.Size, t.enableResume)
	if err != nil {
		return wrapError(ErrKindFilesystem, "failed to prepare temp file", err)
	}
	success := false
	cleanup := func() {
		if success {
			if err := cacheFile.Close(); err != nil {
				logger.Errorf("Failed to close cache file: %v", err)
			}
			return
		}
		if t.enableResume {
			if err := cacheFile.Close(); err != nil {
				logger.Errorf("Failed to close cache file: %v", err)
			}
			return
		}
		if err := cacheFile.CloseAndRemove(); err != nil {
			logger.Errorf("Failed to close/remove cache file: %v", err)
		}
		_ = os.Remove(metaPath)
	}
	defer cleanup()

	meta := newMultipartMeta(metaPath, file.Size)
	if t.enableResume {
		if err := meta.Load(); err != nil {
			logger.Warnf("Failed to load multipart metadata: %v", err)
		}
	}

	partSize := file.Size / int64(t.segmentConfig.maxConcurrency)
	if partSize < t.segmentConfig.minSegmentSize {
		partSize = t.segmentConfig.minSegmentSize
	}
	numParts := (file.Size + partSize - 1) / partSize

	eg, pctx := errgroup.WithContext(ctx)
	eg.SetLimit(t.segmentConfig.maxConcurrency)

	for i := int64(0); i < numParts; i++ {
		start := i * partSize
		end := start + partSize - 1
		if end >= file.Size {
			end = file.Size - 1
		}

		partIndex := i
		chunkStart := start
		chunkEnd := end
		eg.Go(func() error {
			if t.enableResume && meta.IsComplete(partIndex) {
				t.downloadedBytes.Add(chunkEnd - chunkStart + 1)
				file.downloadedBytes.Add(chunkEnd - chunkStart + 1)
				return nil
			}
			return retryWithBackoff(pctx, t.retryPolicy, t.jitter, func() error {
				req, err := t.newRequest(pctx, http.MethodGet, file.URL, nil)
				if err != nil {
					return wrapError(ErrKindValidation, "failed to create ranged GET request", err)
				}
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", chunkStart, chunkEnd))

				resp, err := t.client.Do(req)
				if err != nil {
					return wrapError(ErrKindNetwork, "failed to GET range", err)
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusPartialContent {
					return wrapError(ErrKindHTTP, "server rejected range request", fmt.Errorf("server rejected range request with status %d", resp.StatusCode))
				}

				bufPtr := bufPool.Get().(*[]byte)
				defer bufPool.Put(bufPtr)
				buf := *bufPtr

				currentOffset := chunkStart
				for {
					if err := pctx.Err(); err != nil {
						return err
					}
					if err := t.waitIfPaused(pctx); err != nil {
						return err
					}
					nr, rErr := resp.Body.Read(buf)
					if nr > 0 {
						if t.limiter != nil {
							if err := t.limiter.Wait(pctx, int64(nr)); err != nil {
								return err
							}
						}
						nw, wErr := cacheFile.WriteAt(buf[:nr], currentOffset)
						if wErr != nil {
							return wErr
						}
						if nw != nr {
							return io.ErrShortWrite
						}
						added := int64(nw)
						t.downloadedBytes.Add(added)
						file.downloadedBytes.Add(added)
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
				if t.enableResume {
					meta.MarkComplete(partIndex)
					if err := meta.Save(); err != nil {
						log.FromContext(pctx).Warnf("Failed to persist multipart metadata: %v", err)
					}
				}
				return nil
			})
		})
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	if t.enableResume {
		_ = os.Remove(metaPath)
	}

	if _, err := cacheFile.Seek(0, 0); err != nil {
		return wrapError(ErrKindFilesystem, "failed to rewind cache file", err)
	}

	if err := t.persistToStorage(ctx, file, cacheFile); err != nil {
		return err
	}

	success = true
	if removeErr := cacheFile.CloseAndRemove(); removeErr != nil {
		logger.Errorf("Failed to close/remove cache file: %v", removeErr)
	}

	return nil
}

func (t *Task) processLinkSingle(ctx context.Context, file *File) error {
	logger := log.FromContext(ctx)
	cachePath := filepath.Join(config.C().Temp.BasePath, fmt.Sprintf("direct_%s_%s.part", t.ID, file.Name))

	return retryWithBackoff(ctx, t.retryPolicy, t.jitter, func() (err error) {
		var cacheFile *fsutil.File
		var existingSize int64
		if t.stream {
			cacheFile = nil
		} else {
			cacheFile, err = openResumeFile(cachePath, file.Size, t.enableResume)
			if err != nil {
				return wrapError(ErrKindFilesystem, "failed to create temp file", err)
			}
			stat, statErr := cacheFile.Stat()
			if statErr == nil {
				existingSize = stat.Size()
			}
		}
		if cacheFile != nil {
			defer func() {
				if err == nil {
					return
				}
				if t.enableResume {
					if closeErr := cacheFile.Close(); closeErr != nil {
						logger.Errorf("Failed to close cache file: %v", closeErr)
					}
					return
				}
				if removeErr := cacheFile.CloseAndRemove(); removeErr != nil {
					logger.Errorf("Failed to close/remove cache file: %v", removeErr)
				}
			}()
		}

		req, err := t.newRequest(ctx, http.MethodGet, file.URL, nil)
		if err != nil {
			return wrapError(ErrKindValidation, "failed to create GET request", err)
		}
		if t.enableResume && file.IsResumable && existingSize > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existingSize))
		}

		resp, err := t.client.Do(req)
		if err != nil {
			return wrapError(ErrKindNetwork, "failed to GET", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return wrapError(ErrKindHTTP, "GET returned non-success", fmt.Errorf("GET %s returned status %d", file.URL, resp.StatusCode))
		}

		if t.stream {
			ctx = context.WithValue(ctx, ctxkey.ContentLength, file.Size)
			return t.Storage.Save(ctx, resp.Body, filepath.Join(t.StorPath, file.StorageFileName))
		}

		if t.enableResume && file.IsResumable && existingSize > 0 {
			if _, err := cacheFile.Seek(existingSize, 0); err != nil {
				return wrapError(ErrKindFilesystem, "failed to seek cache file", err)
			}
			current := file.downloadedBytes.Load()
			if existingSize > current {
				delta := existingSize - current
				t.downloadedBytes.Add(delta)
				file.downloadedBytes.Add(delta)
			}
		}

		bufPtr := bufPool.Get().(*[]byte)
		defer bufPool.Put(bufPtr)
		buf := *bufPtr

		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := t.waitIfPaused(ctx); err != nil {
				return err
			}
			nr, rErr := resp.Body.Read(buf)
			if nr > 0 {
				if t.limiter != nil {
					if err := t.limiter.Wait(ctx, int64(nr)); err != nil {
						return err
					}
				}
				nw, wErr := cacheFile.Write(buf[:nr])
				if wErr != nil {
					return wErr
				}
				if nw != nr {
					return io.ErrShortWrite
				}
				t.downloadedBytes.Add(int64(nw))
				file.downloadedBytes.Add(int64(nw))
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
			return wrapError(ErrKindFilesystem, "failed to seek cache file", err)
		}

		if err := t.persistToStorage(ctx, file, cacheFile); err != nil {
			return err
		}

		if err := cacheFile.CloseAndRemove(); err != nil {
			logger.Errorf("Failed to close and remove cache file: %v", err)
		}

		return nil
	})
}

func (t *Task) persistToStorage(ctx context.Context, file *File, reader io.ReadSeeker) error {
	algo := strings.TrimSpace(config.C().Directlinks.ChecksumAlgorithm)
	hasher, err := newHasher(algo)
	if err != nil {
		return wrapError(ErrKindValidation, "invalid checksum algorithm", err)
	}
	if hasher == nil && strings.TrimSpace(config.C().Directlinks.ExpectedChecksum) != "" {
		return wrapError(ErrKindValidation, "checksum algorithm required", errors.New("expected checksum provided without algorithm"))
	}
	if hasher != nil {
		if _, err := io.Copy(hasher, reader); err != nil {
			return wrapError(ErrKindChecksum, "failed to hash file", err)
		}
		if _, err := reader.Seek(0, 0); err != nil {
			return wrapError(ErrKindFilesystem, "failed to rewind after checksum", err)
		}
	}

	storagePath := filepath.Join(t.StorPath, file.StorageFileName)
	if err := t.Storage.Save(ctx, reader, storagePath); err != nil {
		return wrapError(ErrKindStorage, "failed to save to storage", err)
	}

	if hasher != nil {
		checksum := hex.EncodeToString(hasher.Sum(nil))
		if expected := strings.TrimSpace(config.C().Directlinks.ExpectedChecksum); expected != "" && !strings.EqualFold(expected, checksum) {
			return wrapError(ErrKindChecksum, "checksum mismatch", fmt.Errorf("expected %s, got %s", expected, checksum))
		}
		if config.C().Directlinks.WriteChecksumFile {
			algoName := strings.ToLower(strings.TrimSpace(algo))
			if algoName == "" {
				algoName = "checksum"
			}
			checksumPath := storagePath + "." + algoName
			sumReader := strings.NewReader(fmt.Sprintf("%s  %s\n", checksum, file.StorageFileName))
			if err := t.Storage.Save(ctx, sumReader, checksumPath); err != nil {
				log.FromContext(ctx).Warnf("Failed to write checksum file: %v", err)
			}
		}
	}

	return nil
}

func openResumeFile(path string, size int64, enableResume bool) (*fsutil.File, error) {
	if !enableResume {
		cacheFile, err := fsutil.CreateFile(path)
		if err != nil {
			return nil, err
		}
		if size > 0 {
			if err := cacheFile.Truncate(size); err != nil {
				return nil, err
			}
		}
		return cacheFile, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	cacheFile := &fsutil.File{File: file}
	stat, err := file.Stat()
	if err == nil && stat.Size() > size && size > 0 {
		_ = cacheFile.CloseAndRemove()
		return fsutil.CreateFile(path)
	}
	if size > 0 && stat.Size() < size {
		if err := cacheFile.Truncate(size); err != nil {
			return nil, err
		}
	}
	return cacheFile, nil
}

func (t *Task) newRequest(ctx context.Context, method, rawURL string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	if t.userAgent != "" {
		req.Header.Set("User-Agent", t.userAgent)
	}
	if t.authUsername != "" || t.authPassword != "" {
		req.SetBasicAuth(t.authUsername, t.authPassword)
	}
	return req, nil
}

func (t *Task) resolveStoragePath(ctx context.Context, name string) (string, error) {
	clean := SanitizeFilename(name)
	if clean == "" {
		clean = fmt.Sprintf("download_%s", t.ID)
	}

	target := clean
	ext := filepath.Ext(clean)
	base := strings.TrimSuffix(clean, ext)

	switch t.overwritePolicy {
	case overwritePolicyOverwrite:
		return target, nil
	case overwritePolicySkip:
		if t.Storage.Exists(ctx, filepath.Join(t.StorPath, target)) {
			return "", wrapError(ErrKindValidation, "file exists and overwrite policy is skip", fmt.Errorf("file %s already exists", target))
		}
		return target, nil
	default:
		for i := 0; i < 1000; i++ {
			path := filepath.Join(t.StorPath, target)
			if !t.Storage.Exists(ctx, path) {
				return target, nil
			}
			target = fmt.Sprintf("%s (%d)%s", base, i+1, ext)
		}
		return "", wrapError(ErrKindFilesystem, "failed to resolve unique filename", fmt.Errorf("unable to resolve unique name for %s", name))
	}
}

func (t *Task) refreshTransportProxy() error {
	proxyValue := strings.TrimSpace(config.C().Directlinks.Proxy)
	if proxyValue == "" {
		return nil
	}
	parsed, err := url.Parse(proxyValue)
	if err != nil {
		return err
	}
	transport, ok := t.client.Transport.(*http.Transport)
	if !ok {
		return errors.New("http transport is not configurable")
	}
	transport.Proxy = http.ProxyURL(parsed)
	return nil
}

type multipartMeta struct {
	path     string
	complete map[int64]bool
	mu       sync.Mutex
}

func newMultipartMeta(path string, totalSize int64) *multipartMeta {
	return &multipartMeta{
		path:     path,
		complete: make(map[int64]bool),
	}
}

func (m *multipartMeta) Load() error {
	data, err := os.ReadFile(m.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		part, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			continue
		}
		m.complete[part] = true
	}
	return nil
}

func (m *multipartMeta) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var builder strings.Builder
	for part := range m.complete {
		builder.WriteString(strconv.FormatInt(part, 10))
		builder.WriteString("\n")
	}
	return os.WriteFile(m.path, []byte(builder.String()), 0o644)
}

func (m *multipartMeta) MarkComplete(part int64) {
	m.mu.Lock()
	m.complete[part] = true
	m.mu.Unlock()
}

func (m *multipartMeta) IsComplete(part int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.complete[part]
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
