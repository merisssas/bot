package ytdlp

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	ytdlp "github.com/lrstanley/go-ytdlp"

	"github.com/merisssas/Bot/config"
	"github.com/merisssas/Bot/pkg/enums/ctxkey"
	"github.com/merisssas/Bot/storage"
)

// Execute implements core.Executable.
func (t *Task) Execute(ctx context.Context) error {
	logger := t.taskLogger(ctx)
	defer t.closeLogFile()
	logger.Infof("🚀 Starting yt-dlp task %s", t.ID)

	if t.Progress != nil {
		t.Progress.OnStart(ctx, t)
	}

	t.initResilience()

	basePath := config.C().Temp.BasePath
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return t.handleError(ctx, logger, newTaskError(ErrorCodeWorkspace, "create base path", err))
	}

	taskDir, cleanup, err := t.resolveTaskDir(basePath)
	if err != nil {
		return t.handleError(ctx, logger, newTaskError(ErrorCodeWorkspace, "create task workspace", err))
	}
	if cleanup {
		defer func() {
			logger.Debugf("🧹 Cleaning workspace: %s", taskDir)
			_ = os.RemoveAll(taskDir)
		}()
	}

	if t.Config.DryRun {
		if err := t.runDryRun(ctx, logger); err != nil {
			return t.handleError(ctx, logger, err)
		}
		logger.Infof("✅ Dry-run completed for task %s", t.ID)
		if t.Progress != nil {
			t.Progress.OnDone(ctx, t, nil)
		}
		return nil
	}

	downloadedFiles, err := t.downloadQueue(ctx, logger, taskDir)
	if err != nil {
		return t.handleError(ctx, logger, err)
	}

	downloadedFiles, err = t.filterDuplicates(logger, downloadedFiles)
	if err != nil {
		return t.handleError(ctx, logger, err)
	}

	if len(downloadedFiles) == 0 {
		return t.handleError(ctx, logger, newTaskError(ErrorCodeDownloadFailed, "validate download", errors.New("no files produced")))
	}

	logger.Infof("📦 Transferring %d artifact(s) to %s", len(downloadedFiles), t.Storage.Name())

	for _, filePath := range downloadedFiles {
		err = t.retry(ctx, logger, "Transfer Phase", func(_ int) error {
			return t.transferFile(ctx, logger, filePath)
		})
		if err != nil {
			return t.handleError(ctx, logger, err)
		}
	}

	logger.Infof("✅ Task %s completed.", t.ID)
	if t.Progress != nil {
		t.Progress.OnDone(ctx, t, nil)
	}

	return nil
}

func (t *Task) runDryRun(ctx context.Context, logger *log.Logger) error {
	for _, url := range t.URLs {
		if err := t.waitIfPaused(ctx); err != nil {
			return newTaskError(ErrorCodeCanceled, "dry-run paused", err)
		}

		logger.Infof("🔎 Dry-run probe: %s", url)
		cmd := t.buildCommand()
		cmd.SkipDownload().
			DumpSingleJSON().
			NoOverwrites()

		args := append(t.Flags, url)
		if _, err := cmd.Run(ctx, args...); err != nil {
			return newTaskError(ErrorCodeDownloadFailed, "dry-run probe", err)
		}
	}

	return nil
}

func (t *Task) downloadQueue(ctx context.Context, logger *log.Logger, taskDir string) ([]string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	workerCount := t.Config.DownloadConcurrency
	if workerCount < 1 {
		workerCount = 1
	}

	items := t.buildQueueItems()

	type result struct {
		files []string
		err   error
	}

	jobs := make(chan queueItem)
	results := make(chan result)

	var wg sync.WaitGroup
	worker := func() {
		defer wg.Done()
		for item := range jobs {
			if err := t.waitIfPaused(ctx); err != nil {
				results <- result{err: newTaskError(ErrorCodeCanceled, "paused", err)}
				continue
			}

			var files []string
			err := t.retry(ctx, logger, fmt.Sprintf("Download %d/%d", item.index+1, len(items)), func(attempt int) error {
				var dErr error
				files, dErr = t.downloadSingle(ctx, logger, taskDir, item, attempt)
				return dErr
			})

			results <- result{files: files, err: err}
		}
	}

	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go worker()
	}

	go func() {
		for _, item := range items {
			jobs <- item
		}
		close(jobs)
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var files []string
	var firstErr error
	for res := range results {
		if res.err != nil && firstErr == nil {
			firstErr = res.err
			cancel()
		}
		files = append(files, res.files...)
	}

	if firstErr != nil {
		return files, firstErr
	}

	return files, nil
}

func (t *Task) downloadSingle(ctx context.Context, logger *log.Logger, taskDir string, item queueItem, attempt int) ([]string, error) {
	subDir, cleanup, err := t.prepareURLDir(taskDir, item.index)
	if err != nil {
		return nil, err
	}
	if cleanup {
		defer func() {
			_ = os.RemoveAll(subDir)
		}()
	}

	host := hostFromURL(item.url)
	if err := t.waitForRateLimit(ctx, host); err != nil {
		return nil, err
	}

	var lastErr error
	for _, plan := range t.buildAttemptPlans(item.url, attempt) {
		files, err := t.runDownloadAttempt(ctx, logger, subDir, item, plan)
		if err == nil {
			return files, nil
		}
		lastErr = err
		if !errors.Is(err, errFormatUnavailable) {
			return nil, err
		}
		logger.Warnf("Format unavailable for %s, trying fallback format", item.url)
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, newTaskError(ErrorCodeDownloadFailed, "download", errors.New("no valid file produced"))
}

func (t *Task) transferFile(ctx context.Context, logger *log.Logger, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return newTaskError(ErrorCodeTransferFailed, "open artifact", err)
	}
	defer f.Close()

	fileInfo, err := f.Stat()
	if err != nil {
		return newTaskError(ErrorCodeTransferFailed, "stat artifact", err)
	}

	ctx = context.WithValue(ctx, ctxkey.ContentLength, fileInfo.Size())

	fileName := sanitizeFilename(filepath.Base(filePath))
	destPath, err := resolveDestinationPath(ctx, t.Storage, filepath.Join(t.StorPath, fileName), t.Config.OverwritePolicy)
	if err != nil {
		return newTaskError(ErrorCodeTransferFailed, "resolve destination", err)
	}
	if destPath == "" {
		logger.Infof("⏭️ Skipping upload for %s due to overwrite policy", fileName)
		return nil
	}

	logger.Infof("⬆️ Uploading: %s -> %s", fileName, destPath)
	if err := t.Storage.Save(ctx, f, destPath); err != nil {
		return newTaskError(ErrorCodeTransferFailed, "storage save", err)
	}

	if t.Progress != nil {
		t.Progress.OnProgress(ctx, t, ProgressUpdate{
			Status:       fmt.Sprintf("uploaded %s", fileName),
			FilePercent:  100,
			TotalPercent: t.totalPercent(100),
			Filename:     fileName,
			ItemIndex:    t.Stats.CompletedURLs,
			ItemTotal:    t.Stats.TotalURLs,
		})
	}

	return nil
}

func (t *Task) retry(ctx context.Context, logger *log.Logger, operation string, fn func(attempt int) error) error {
	var err error
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	policy := RetryPolicy{
		MaxAttempts:            t.Config.MaxRetries,
		BaseDelay:              t.Config.RetryBaseDelay,
		MaxDelay:               t.Config.RetryMaxDelay,
		JitterFactor:           t.Config.RetryJitter,
		RateLimitMultiplier:    3,
		ServerErrorMultiplier:  2,
		NetworkErrorMultiplier: 2,
	}

	for attempt := 0; attempt <= t.Config.MaxRetries; attempt++ {
		if ctx.Err() != nil {
			return newTaskError(ErrorCodeCanceled, operation, ctx.Err())
		}

		if attempt > 0 {
			retryClass := classifyRetryError(err)
			delay, ok := policy.NextDelay(attempt, retryClass)
			if !ok {
				break
			}
			jitter := time.Duration(rng.Float64() * policy.JitterFactor * float64(delay))
			wait := delay + jitter
			logger.Warnf("⚠️ %s failed. Retrying in %s (Attempt %d/%d)... Error: %v", operation, wait, attempt, t.Config.MaxRetries, err)

			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return newTaskError(ErrorCodeCanceled, operation, ctx.Err())
			}
		}

		err = fn(attempt)
		if err == nil {
			return nil
		}
	}
	return newTaskError(ErrorCodeDownloadFailed, operation, err)
}

func (t *Task) handleError(ctx context.Context, logger *log.Logger, err error) error {
	logger.Error(err.Error())
	if t.Progress != nil {
		t.Progress.OnDone(ctx, t, err)
	}
	return err
}

func (t *Task) buildCommand() *ytdlp.Command {
	cmd := ytdlp.New()
	cmd.Newline()
	return cmd
}

type queueItem struct {
	index    int
	url      string
	priority int
}

func (t *Task) buildQueueItems() []queueItem {
	items := make([]queueItem, 0, len(t.URLs))
	for idx, url := range t.URLs {
		items = append(items, queueItem{
			index:    idx,
			url:      url,
			priority: t.Config.Priority,
		})
	}

	if raw, ok := t.Meta["priorities"]; ok {
		switch priorities := raw.(type) {
		case map[string]int:
			for i := range items {
				if prio, exists := priorities[items[i].url]; exists {
					items[i].priority = prio
				}
			}
		case []int:
			for i := range items {
				if i < len(priorities) {
					items[i].priority = priorities[i]
				}
			}
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].priority == items[j].priority {
			return items[i].index < items[j].index
		}
		return items[i].priority > items[j].priority
	})

	return items
}

func collectValidFiles(logger *log.Logger, tempDir string) ([]string, error) {
	files, err := os.ReadDir(tempDir)
	if err != nil {
		return nil, err
	}

	var validFiles []string
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fName := file.Name()
		if isPartialFile(fName) {
			continue
		}

		fullPath := filepath.Join(tempDir, fName)
		info, err := file.Info()
		if err != nil {
			logger.Warnf("Skipping file %s: %v", fName, err)
			continue
		}
		if info.Size() < 1024 {
			logger.Warnf("Skipping suspicious small file: %s (%d bytes)", fName, info.Size())
			continue
		}

		validFiles = append(validFiles, fullPath)
		logger.Debugf("Target acquired: %s (%s)", fName, humanizeBytes(info.Size()))
	}

	return validFiles, nil
}

func isPartialFile(name string) bool {
	return strings.HasSuffix(name, ".part") ||
		strings.HasSuffix(name, ".ytdl") ||
		strings.HasSuffix(name, ".temp") ||
		strings.Contains(name, ".f137") ||
		strings.Contains(name, ".f140")
}

func hasUnmergedStreams(tempDir string) bool {
	files, err := os.ReadDir(tempDir)
	if err != nil {
		return false
	}

	streamPattern := regexp.MustCompile(`\.f\d+\.`)
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if streamPattern.MatchString(file.Name()) {
			return true
		}
	}

	return false
}

func summarizeYtdlpFailure(runErr error, result *ytdlp.Result) string {
	if result == nil {
		return ""
	}

	stderrLine := lastNonEmptyLine(result.Stderr)
	if stderrLine != "" {
		return fmt.Sprintf("yt-dlp stderr: %s", stderrLine)
	}

	stdoutLine := lastNonEmptyLine(result.Stdout)
	if stdoutLine != "" {
		return fmt.Sprintf("yt-dlp output: %s", stdoutLine)
	}

	if runErr != nil {
		return fmt.Sprintf("yt-dlp error: %v", runErr)
	}

	return ""
}

func lastNonEmptyLine(text string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}

func (t *Task) verifyFiles(logger *log.Logger, files []string) ([]string, error) {
	var validated []string
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			return nil, newTaskError(ErrorCodeIntegrity, "stat file", err)
		}
		if info.Size() == 0 {
			logger.Warnf("Removing empty file: %s", file)
			_ = os.Remove(file)
			continue
		}

		if t.Config.ChecksumAlgorithm != "" {
			sum, err := checksumFile(file, t.Config.ChecksumAlgorithm)
			if err != nil {
				return nil, newTaskError(ErrorCodeIntegrity, "checksum", err)
			}
			if t.Config.ExpectedChecksum != "" && !strings.EqualFold(sum, t.Config.ExpectedChecksum) {
				_ = os.Remove(file)
				return nil, newTaskError(ErrorCodeIntegrity, "checksum mismatch", fmt.Errorf("expected %s got %s", t.Config.ExpectedChecksum, sum))
			}

			if t.Config.WriteChecksumFile {
				if err := writeChecksumFile(file, t.Config.ChecksumAlgorithm, sum); err != nil {
					return nil, newTaskError(ErrorCodeIntegrity, "write checksum", err)
				}
				validated = append(validated, file+"."+t.Config.ChecksumAlgorithm)
			}
		}

		validated = append(validated, file)
	}

	return validated, nil
}

func checksumFile(path, algorithm string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var hash hashWriter
	switch strings.ToLower(algorithm) {
	case "sha256":
		hash = sha256.New()
	case "sha1":
		hash = sha1.New()
	case "md5":
		hash = md5.New()
	default:
		return "", fmt.Errorf("unsupported checksum algorithm: %s", algorithm)
	}

	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type hashWriter interface {
	io.Writer
	Sum([]byte) []byte
}

func writeChecksumFile(path, algorithm, sum string) error {
	checksumPath := path + "." + strings.ToLower(algorithm)
	content := fmt.Sprintf("%s  %s\n", sum, filepath.Base(path))
	return os.WriteFile(checksumPath, []byte(content), 0644)
}

func resolveDestinationPath(ctx context.Context, stor storage.Storage, destPath string, policy OverwritePolicy) (string, error) {
	if !stor.Exists(ctx, destPath) {
		return destPath, nil
	}

	switch policy {
	case OverwritePolicyOverwrite:
		return destPath, nil
	case OverwritePolicySkip:
		return "", nil
	default:
		return autoRenamePath(ctx, stor, destPath)
	}
}

func autoRenamePath(ctx context.Context, stor storage.Storage, destPath string) (string, error) {
	dir := filepath.Dir(destPath)
	base := filepath.Base(destPath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	for i := 1; i <= 1000; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", name, i, ext))
		if !stor.Exists(ctx, candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("failed to auto-rename after 1000 attempts")
}

func (t *Task) updateStats(url string, percent float64, status string) {
	t.statsMu.Lock()
	defer t.statsMu.Unlock()
	t.Stats.ActiveURL = url
	t.Stats.ActivePercent = percent
	t.Stats.LastUpdate = time.Now()
	t.Stats.LastError = status
}

func (t *Task) markComplete(url string) {
	t.statsMu.Lock()
	defer t.statsMu.Unlock()
	t.Stats.CompletedURLs++
	t.Stats.ActiveURL = url
	t.Stats.ActivePercent = 100
	t.Stats.LastUpdate = time.Now()
}

func (t *Task) totalPercent(currentPercent float64) float64 {
	t.statsMu.Lock()
	defer t.statsMu.Unlock()
	total := t.Stats.TotalURLs
	if total <= 0 {
		return currentPercent
	}
	completed := float64(t.Stats.CompletedURLs)
	return ((completed + (currentPercent / 100)) / float64(total)) * 100
}

func (t *Task) taskLogger(ctx context.Context) *log.Logger {
	logger := log.FromContext(ctx).WithPrefix("ytdlp")
	logger.SetLevel(t.Config.LogLevel)

	if strings.TrimSpace(t.Config.LogFile) == "" {
		return logger
	}

	file, err := os.OpenFile(t.Config.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logger.Warnf("Failed to open log file %s: %v", t.Config.LogFile, err)
		return logger
	}

	t.logFile = file
	logger.SetOutput(io.MultiWriter(os.Stderr, file))
	return logger
}

func (t *Task) closeLogFile() {
	if t.logFile == nil {
		return
	}
	_ = t.logFile.Close()
	t.logFile = nil
}

func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, ":", "_")
	name = strings.ReplaceAll(name, "\"", "'")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	return name
}

func humanizeBytes(s int64) string {
	sizes := []string{"B", "KB", "MB", "GB", "TB"}
	if s == 0 {
		return "0 B"
	}
	i := int(math.Floor(math.Log(float64(s)) / math.Log(1024)))
	if i >= len(sizes) {
		i = len(sizes) - 1
	}
	val := float64(s) / math.Pow(1024, float64(i))
	return fmt.Sprintf("%.1f %s", val, sizes[i])
}

func calcSpeed(downloaded int, duration time.Duration) string {
	if duration <= 0 {
		return "0 B/s"
	}
	bytesPerSec := float64(downloaded) / duration.Seconds()
	return humanizeBytes(int64(bytesPerSec)) + "/s"
}
