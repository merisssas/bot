package ytdlp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	ytdlp "github.com/lrstanley/go-ytdlp"
	"golang.org/x/sync/errgroup"

	"github.com/merisssas/bot/common/utils/dlutil"
	"github.com/merisssas/bot/config"
	"github.com/merisssas/bot/pkg/enums/ctxkey"
)

const (
	baseDelay       = 1 * time.Second
	maxDelay        = 45 * time.Second
	maxRetries      = 6
	multiplier      = 2.0
	jitterFactor    = 0.25
	minDiskSpace    = 500 * 1024 * 1024
	downloadTimeout = 2 * time.Hour
)

var (
	rngMu sync.Mutex
	rng   = rand.New(rand.NewSource(time.Now().UnixNano()))
)

// Execute implements core.Executable with resilient processing.
func (t *Task) Execute(ctx context.Context) error {
	logger := log.FromContext(ctx).With("task_id", t.ID)
	logger.Infof("Starting yt-dlp task")

	if t.Progress != nil {
		t.Progress.OnStart(ctx, t)
	}

	if err := t.preflight(logger); err != nil {
		t.notifyError(ctx, err)
		return err
	}

	tempDir, err := t.setupWorkspace(logger)
	if err != nil {
		t.notifyError(ctx, err)
		return err
	}
	defer func() {
		if cleanupErr := os.RemoveAll(tempDir); cleanupErr != nil {
			logger.Warnf("Failed to cleanup temp dir: %v", cleanupErr)
		}
	}()

	if err := acquireDownloadSlot(ctx); err != nil {
		logger.Errorf("Failed to acquire download slot: %v", err)
		t.notifyError(ctx, err)
		return err
	}
	defer releaseDownloadSlot()

	downloadedFiles, err := t.downloadWithSmartRetry(ctx, tempDir, logger)
	if err != nil {
		t.notifyError(ctx, err)
		return err
	}

	if len(downloadedFiles) == 0 {
		err := errors.New("download completed but no artifacts found")
		logger.Error(err.Error())
		t.notifyError(ctx, err)
		return err
	}

	logger.Infof("Processing %d file(s) in parallel pipelines", len(downloadedFiles))
	eg, egCtx := errgroup.WithContext(ctx)

	if t.UploadToChat {
		eg.Go(func() error {
			logger.Debug("Starting Telegram upload pipeline")
			if err := t.uploadFilesToChat(egCtx, downloadedFiles, tempDir); err != nil {
				return fmt.Errorf("telegram upload failed: %w", err)
			}
			logger.Debug("Telegram upload pipeline completed")
			return nil
		})
	}

	if t.Storage != nil {
		eg.Go(func() error {
			logger.Debug("Starting storage transfer pipeline")
			if err := t.processStorageTransfer(egCtx, downloadedFiles); err != nil {
				return fmt.Errorf("storage transfer failed: %w", err)
			}
			logger.Debug("Storage transfer pipeline completed")
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		logger.Errorf("Pipeline execution failed: %v", err)
		t.notifyError(ctx, err)
		return err
	}

	logger.Infof("yt-dlp task %s completed successfully", t.ID)
	if t.Progress != nil {
		t.Progress.OnDone(ctx, t, nil)
	}

	return nil
}

func (t *Task) preflight(logger *log.Logger) error {
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		logger.Warn("yt-dlp binary not found in PATH; downloads will fail until installed")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		logger.Warn("ffmpeg not found in PATH; some post-processing may fail")
	}

	basePath := config.C().Temp.BasePath
	if basePath == "" {
		basePath = os.TempDir()
	}
	free, err := getFreeDiskSpace(basePath)
	if err != nil {
		logger.Warnf("Unable to check free disk space: %v", err)
		return nil
	}
	if free < minDiskSpace {
		return fmt.Errorf("insufficient disk space in %s: available %s, need %s", basePath, dlutil.FormatSize(free), dlutil.FormatSize(minDiskSpace))
	}
	return nil
}

type failureClass int

const (
	failUnknown failureClass = iota
	failAuthRequired
	failRateLimited
	failSignatureNsig
	failPostProcess
	failTransientNetwork
)

func classifyFailure(res *ytdlp.Result, err error) (failureClass, string) {
	if err == nil {
		return failUnknown, ""
	}
	msg := errorMessage(res, err)

	switch {
	case strings.Contains(msg, "sign in to confirm your age") ||
		(strings.Contains(msg, "cookies") && strings.Contains(msg, "required")):
		return failAuthRequired, "Blocked: authentication/cookies required. Provide cookies file."
	case strings.Contains(msg, "http error 429") ||
		strings.Contains(msg, "too many requests"):
		return failRateLimited, "Rate-limited (HTTP 429). Switching to conservative sleep + lower concurrency."
	case strings.Contains(msg, "nsig extraction failed") ||
		strings.Contains(msg, "signature extraction failed"):
		return failSignatureNsig, "Signature/nsig failed. Applying YouTube client fallback + urging yt-dlp update."
	case strings.Contains(msg, "postprocessing") ||
		(strings.Contains(msg, "ffmpeg") && strings.Contains(msg, "error")):
		return failPostProcess, "Postprocess failed. Retrying with reduced post-processing to salvage media."
	case strings.Contains(msg, "timed out") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "temporary failure") ||
		strings.Contains(msg, "http error 5"):
		return failTransientNetwork, "Transient network error. Retrying with backoff."
	default:
		return failUnknown, ""
	}
}

func (t *Task) downloadWithSmartRetry(ctx context.Context, tempDir string, logger *log.Logger) ([]string, error) {
	plan, err := t.buildDownloadPlan(logger, tempDir)
	if err != nil {
		return nil, err
	}

	baseCmd := ytdlp.New().
		SetSeparateProcessGroup(true).
		Output(filepath.Join(tempDir, "%(title).200B [%(id)s].%(ext)s"))

	if t.Progress != nil {
		baseCmd = baseCmd.ProgressFunc(500*time.Millisecond, func(update ytdlp.ProgressUpdate) {
			status := formatProgressStatus(update)
			if status != "" {
				t.Progress.OnProgress(ctx, t, status)
			}
		})
	}

	var lastErr error
	var lastClass failureClass
	var lastHint string
	for attempt := 1; attempt <= maxRetries; attempt++ {
		logger.Infof("Attempt %d/%d: executing download strategy", attempt, maxRetries)
		if err := os.MkdirAll(plan.cacheDir, 0o755); err != nil {
			logger.Warnf("Failed to ensure cache dir %s: %v", plan.cacheDir, err)
		}

		args := plan.argsForAttempt(attempt, lastClass)
		cmdCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
		res, cmdErr := baseCmd.Run(cmdCtx, args...)
		cancel()
		if cmdErr == nil {
			return t.scanDownloadedFiles(tempDir, logger)
		}

		lastErr = cmdErr
		if errors.Is(cmdErr, context.Canceled) {
			return nil, cmdErr
		}

		if isFatalError(res, cmdErr) {
			return nil, cmdErr
		}

		failureKind, hint := classifyFailure(res, cmdErr)
		lastClass = failureKind
		if hint != "" {
			lastHint = hint
			logger.Warn(hint)
		}

		if failureKind == failAuthRequired {
			if lastHint != "" {
				return nil, fmt.Errorf("%w (%s)", cmdErr, lastHint)
			}
			return nil, cmdErr
		}

		delay := calculateBackoff(attempt)
		if failureKind == failRateLimited {
			delay += 20 * time.Second
			if delay > 2*time.Minute {
				delay = 2 * time.Minute
			}
		}
		logger.Warnf("Download failed: %v. Retrying in %s...", cmdErr, delay)
		if err := sleepWithContext(ctx, delay); err != nil {
			return nil, err
		}
	}

	if lastHint != "" {
		return nil, fmt.Errorf("exhausted all %d attempts: %w (last_hint=%s)", maxRetries, lastErr, lastHint)
	}
	return nil, fmt.Errorf("exhausted all %d attempts: %w", maxRetries, lastErr)
}

func (t *Task) processStorageTransfer(ctx context.Context, files []string) error {
	eg := errgroup.Group{}
	limit := config.C().Workers
	if limit < 1 {
		limit = 4
	}
	eg.SetLimit(limit)

	for _, filePath := range files {
		filePath := filePath
		eg.Go(func() error {
			return t.transferFile(ctx, filePath)
		})
	}
	return eg.Wait()
}

type downloadPlan struct {
	baseArgs []string
	userArgs []string
	urls     []string
	cacheDir string
}

func (p *downloadPlan) argsForAttempt(attempt int, last failureClass) []string {
	args := make([]string, 0, len(p.baseArgs)+len(p.userArgs)+len(p.urls)+32)
	args = append(args, p.baseArgs...)
	args = append(args, p.userArgs...)

	switch {
	case attempt >= 2 && (last == failSignatureNsig || last == failUnknown):
		args = append(args, "--extractor-args", "youtube:player_client=android,web")
	case last == failRateLimited:
		args = append(args,
			"--sleep-requests", "1",
			"--sleep-interval", "1",
			"--max-sleep-interval", "5",
			"--concurrent-fragments", "1",
		)
	case last == failPostProcess:
		args = append(args, "--no-embed-subs", "--no-embed-thumbnail")
	case last == failTransientNetwork && attempt >= 3:
		args = append(args, "-4")
	}

	args = append(args, p.urls...)
	return args
}

func (t *Task) buildDownloadPlan(logger *log.Logger, tempDir string) (*downloadPlan, error) {
	safeFlags, err := sanitizeFlags(t.Flags)
	if err != nil {
		return nil, err
	}

	cacheDir := filepath.Join(tempDir, "cache")

	base := []string{
		"--ignore-config",
		"--no-playlist",
		"--no-mtime",
		"--geo-bypass",
		"--cache-dir", cacheDir,
		"--write-info-json",
		"--retries", "10",
		"--fragment-retries", "50",
		"--file-access-retries", "10",
		"--extractor-retries", "10",
		"--retry-sleep", "exp=1:30:2",
		"--concurrent-fragments", "4",
		"--add-metadata",
		"--embed-chapters",
		"--embed-subs",
		"--sub-langs", "en.*,id.*,en,id",
		"--hls-use-mpegts",
		"--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	}

	if !hasAnyFormatFlag(safeFlags) {
		base = append(base,
			"-f", "bv*[ext=mp4]+ba[ext=m4a]/b[ext=mp4]/bv*+ba/b",
			"--merge-output-format", "mp4",
		)
	}

	if t.Cookie != "" {
		if _, err := os.Stat(t.Cookie); err == nil {
			base = append(base, "--cookies", t.Cookie)
		} else {
			logger.Warn("Cookie file specified but not found, proceeding without it")
		}
	}

	if t.UploadToChat {
		base = append(base, "--write-thumbnail", "--convert-thumbnails", "jpg")
	}

	base = append(base, "--embed-thumbnail")

	return &downloadPlan{
		baseArgs: base,
		userArgs: safeFlags,
		urls:     t.URLs,
		cacheDir: cacheDir,
	}, nil
}

func hasAnyFormatFlag(flags []string) bool {
	for _, f := range flags {
		ff := strings.TrimSpace(f)
		if ff == "-f" || strings.HasPrefix(ff, "-f=") || strings.HasPrefix(ff, "-f ") ||
			ff == "--format" || strings.HasPrefix(ff, "--format=") || strings.HasPrefix(ff, "--format ") {
			return true
		}
	}
	return false
}

func (t *Task) scanDownloadedFiles(dir string, logger *log.Logger) ([]string, error) {
	var files []string
	var metadataFiles []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() && filepath.Base(path) == "cache" {
			return filepath.SkipDir
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".info.json") {
			metadataFiles = append(metadataFiles, path)
			return nil
		}
		if isIgnoredDownloadArtifact(info.Name()) {
			logger.Debugf("Skipping non-primary download artifact: %s", info.Name())
			return nil
		}
		if info.Size() == 0 {
			logger.Warnf("Skipping empty artifact: %s", info.Name())
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(metadataFiles) > 0 {
		if tracked := filesFromMetadata(metadataFiles, logger); len(tracked) > 0 {
			return tracked, nil
		}
	}
	return files, nil
}

func (t *Task) setupWorkspace(logger *log.Logger) (string, error) {
	basePath := config.C().Temp.BasePath
	if basePath == "" {
		basePath = os.TempDir()
	}
	if err := os.MkdirAll(basePath, 0o755); err != nil {
		return "", fmt.Errorf("failed to init base path: %w", err)
	}
	tempDir, err := os.MkdirTemp(basePath, "ytdlp-ultimate-*")
	if err != nil {
		return "", fmt.Errorf("failed to create secure temp dir: %w", err)
	}
	logger.Debugf("Workspace initialized: %s", tempDir)
	return tempDir, nil
}

func (t *Task) notifyError(ctx context.Context, err error) {
	if t.Progress != nil {
		t.Progress.OnDone(ctx, t, err)
	}
}

// transferFile transfers a single file to storage
func (t *Task) transferFile(ctx context.Context, filePath string) error {
	logger := log.FromContext(ctx)

	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return err
	}

	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	ctx = context.WithValue(ctx, ctxkey.ContentLength, fileInfo.Size())

	fileName := sanitizeFilename(filepath.Base(filePath))
	destPath := filepath.Join(t.StorPath, fileName)

	logger.Infof("Archiving: %s -> %s", fileName, t.Storage.Name())

	if err := t.Storage.Save(ctx, f, destPath); err != nil {
		return fmt.Errorf("storage save failed for %s: %w", fileName, err)
	}

	if t.Progress != nil {
		t.Progress.OnProgress(ctx, t, fmt.Sprintf("Transferred: %s", fileName))
	}
	return nil
}

func sanitizeFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, name)
	return strings.Trim(name, " .")
}

func sanitizeFlags(flags []string) ([]string, error) {
	if len(flags) == 0 {
		return nil, nil
	}

	forbidden := map[string]bool{
		"--exec": true, "--exec-before-download": true, "--exec-after-download": true,
		"--exec-before": true, "--exec-after": true, "--exec-before-downloads": true,
		"--exec-after-downloads": true, "--output": true, "-o": true,
		"--paths": true, "-P": true, "--config-location": true,
		"--batch-file": true, "--load-info": true, "--plugin-dirs": true,
		"--external-downloader": true, "--external-downloader-args": true,
		"--ffmpeg-location": true,
	}

	safe := make([]string, 0, len(flags))
	for _, flag := range flags {
		clean := strings.TrimSpace(flag)
		if clean == "" {
			continue
		}

		clean = normalizeFlagToken(clean)
		flagName := clean
		if strings.Contains(clean, "=") {
			parts := strings.SplitN(clean, "=", 2)
			flagName = strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			value = stripMatchingQuotes(value)
			clean = fmt.Sprintf("%s=%s", flagName, value)
		}

		if forbidden[flagName] {
			return nil, fmt.Errorf("security violation: flag %q is strictly prohibited", flagName)
		}

		safe = append(safe, clean)
	}
	return safe, nil
}

func normalizeFlagToken(token string) string {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return trimmed
	}
	return stripMatchingQuotes(trimmed)
}

func stripMatchingQuotes(value string) string {
	if len(value) < 2 {
		return value
	}
	first := value[0]
	last := value[len(value)-1]
	if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
		return value[1 : len(value)-1]
	}
	return value
}

func calculateBackoff(attempt int) time.Duration {
	backoff := float64(baseDelay) * math.Pow(multiplier, float64(attempt-1))
	if backoff > float64(maxDelay) {
		backoff = float64(maxDelay)
	}

	rngMu.Lock()
	j := rng.Float64()
	rngMu.Unlock()
	random := j*(jitterFactor*2) + (1 - jitterFactor)
	return time.Duration(backoff * random)
}

func isIgnoredDownloadArtifact(name string) bool {
	lowerName := strings.ToLower(name)
	ignoredSuffixes := []string{
		".part",
		".ytdl",
		".tmp",
		".temp",
		".lock",
		".info.json",
		".description",
		".annotations.xml",
		".vtt",
		".srt",
		".ass",
		".lrc",
		".webp",
		".jpg",
		".jpeg",
		".png",
	}
	for _, suffix := range ignoredSuffixes {
		if strings.HasSuffix(lowerName, suffix) {
			return true
		}
	}
	return false
}

func formatProgressStatus(update ytdlp.ProgressUpdate) string {
	action := "Downloading"
	switch update.Status {
	case ytdlp.ProgressStatusStarting:
		action = "Starting"
	case ytdlp.ProgressStatusPostProcessing:
		action = "Post-processing"
	case ytdlp.ProgressStatusFinished:
		action = "Finished"
	case ytdlp.ProgressStatusError:
		action = "Error"
	}

	title := ""
	if update.Info != nil && update.Info.Title != nil && *update.Info.Title != "" {
		title = *update.Info.Title
	} else if update.Filename != "" {
		title = filepath.Base(update.Filename)
	}

	if title != "" {
		action = fmt.Sprintf("%s: %s", action, title)
	}

	if update.Status == ytdlp.ProgressStatusDownloading {
		percent := update.Percent()
		bar := buildProgressBar(percent, 10)
		speed := formatSpeed(update)
		parts := []string{fmt.Sprintf("%s %s", bar, update.PercentString())}
		if speed != "" {
			parts = append(parts, speed)
		}
		if eta := update.ETA(); eta > 0 {
			parts = append(parts, fmt.Sprintf("ETA %s", dlutil.FormatDuration(eta)))
		}
		return fmt.Sprintf("%s %s", action, strings.Join(parts, " "))
	}

	var details []string
	if update.TotalBytes > 0 {
		details = append(details, fmt.Sprintf("%s/%s", dlutil.FormatSize(int64(update.DownloadedBytes)), dlutil.FormatSize(int64(update.TotalBytes))))
		details = append(details, update.PercentString())
	} else if update.DownloadedBytes > 0 {
		details = append(details, dlutil.FormatSize(int64(update.DownloadedBytes)))
	}
	if update.FragmentCount > 0 {
		details = append(details, fmt.Sprintf("fragment %d/%d", update.FragmentIndex, update.FragmentCount))
	}
	if eta := update.ETA(); eta > 0 {
		details = append(details, fmt.Sprintf("ETA %s", dlutil.FormatDuration(eta)))
	}

	if len(details) == 0 {
		return action
	}
	return fmt.Sprintf("%s (%s)", action, strings.Join(details, ", "))
}

func buildProgressBar(percent float64, width int) string {
	if width <= 0 {
		width = 10
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := int(percent / 100 * float64(width))
	if filled > width {
		filled = width
	}
	return fmt.Sprintf("%s%s", strings.Repeat("█", filled), strings.Repeat("░", width-filled))
}

func formatSpeed(update ytdlp.ProgressUpdate) string {
	duration := update.Duration()
	if duration <= 0 {
		return ""
	}
	downloaded := update.DownloadedBytes
	if downloaded <= 0 {
		return ""
	}
	speed := float64(downloaded) / duration.Seconds()
	if speed <= 0 {
		return ""
	}
	return fmt.Sprintf("%s/s", dlutil.FormatSize(int64(speed)))
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

type ytDlpRequestedDownload struct {
	Filepath string `json:"filepath"`
	Filename string `json:"filename"`
}

type ytDlpMetadata struct {
	Filename           string                   `json:"_filename"`
	RequestedDownloads []ytDlpRequestedDownload `json:"requested_downloads"`
}

func errorMessage(res *ytdlp.Result, err error) string {
	if err == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(strings.ToLower(err.Error()))
	if res != nil {
		if res.Stdout != "" {
			b.WriteString("\n")
			b.WriteString(strings.ToLower(res.Stdout))
		}
		if res.Stderr != "" {
			b.WriteString("\n")
			b.WriteString(strings.ToLower(res.Stderr))
		}
	}
	return b.String()
}

func isFatalError(res *ytdlp.Result, err error) bool {
	msg := errorMessage(res, err)
	fatalKeywords := []string{
		"unsupported url",
		"login required",
		"account terminated",
		"video unavailable",
		"private video",
		"copyright",
		"inappropriate content",
		"geo-restricted",
	}
	for _, keyword := range fatalKeywords {
		if strings.Contains(msg, keyword) {
			return true
		}
	}
	return false
}

func filesFromMetadata(metadataFiles []string, logger *log.Logger) []string {
	tracked := make(map[string]struct{})
	for _, metaPath := range metadataFiles {
		content, err := os.ReadFile(metaPath)
		if err != nil {
			logger.Warnf("Failed to read metadata file %s: %v", metaPath, err)
			continue
		}

		var metadata ytDlpMetadata
		if err := json.Unmarshal(content, &metadata); err != nil {
			logger.Warnf("Failed to parse metadata file %s: %v", metaPath, err)
			continue
		}

		addTrackedPath(tracked, resolveMetadataPath(metaPath, metadata.Filename), logger)
		for _, requested := range metadata.RequestedDownloads {
			if requested.Filepath != "" {
				addTrackedPath(tracked, resolveMetadataPath(metaPath, requested.Filepath), logger)
				continue
			}
			addTrackedPath(tracked, resolveMetadataPath(metaPath, requested.Filename), logger)
		}
	}

	if len(tracked) == 0 {
		return nil
	}

	files := make([]string, 0, len(tracked))
	for path := range tracked {
		files = append(files, path)
	}
	return files
}

func addTrackedPath(tracked map[string]struct{}, candidate string, logger *log.Logger) {
	if candidate == "" {
		return
	}
	if isIgnoredDownloadArtifact(filepath.Base(candidate)) {
		return
	}
	info, err := os.Stat(candidate)
	if err != nil {
		logger.Debugf("Skipping missing artifact %s: %v", candidate, err)
		return
	}
	if info.Size() == 0 {
		logger.Warnf("Skipping empty artifact: %s", candidate)
		return
	}
	tracked[candidate] = struct{}{}
}

func resolveMetadataPath(metaPath, candidate string) string {
	if candidate == "" {
		return ""
	}
	if filepath.IsAbs(candidate) {
		return candidate
	}
	return filepath.Join(filepath.Dir(metaPath), candidate)
}

func getFreeDiskSpace(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}
