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
	"runtime"
	"sort"
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

// --- Configuration Constants ---
const (
	// Resilience Settings
	baseDelay       = 1 * time.Second
	maxDelay        = 45 * time.Second // Slightly faster retry cycle
	maxRetries      = 10               // Aggressive retry capability
	multiplier      = 1.5
	jitterFactor    = 0.5
	downloadTimeout = 6 * time.Hour // HLS merging can take time

	// Resource Safety
	minDiskSpace = 500 * 1024 * 1024 // 500MB headroom

	// "Native Woosh" HLS Settings
	hlsConcurrentFragments = "16"  // The Sweet Spot for HLS speed
	hlsMaxFragmentRetries  = "100" // Never give up on a segment
	httpChunkSize          = "10M" // Keep the pipe full
)

var (
	rngMu sync.Mutex
	rng   = rand.New(rand.NewSource(time.Now().UnixNano()))
	// Cache capability checks
	hasFFmpeg bool
	initOnce  sync.Once
)

// Execute implements core.Executable with Native Turbo Engine.
func (t *Task) Execute(ctx context.Context) error {
	logger := log.FromContext(ctx).With("task_id", t.ID)

	// Initialize capability flags once
	initOnce.Do(func() {
		_, errFF := exec.LookPath("ffmpeg")
		hasFFmpeg = errFF == nil

		if hasFFmpeg {
			logger.Info("🚀 Native Turbo Engine: STARTED. FFmpeg detected for merging.")
		} else {
			logger.Warn("⚠️ Performance Warning: FFmpeg not found. Merging speed will be impacted.")
		}
	})

	logger.Infof("Starting High-Performance Native Downloader")

	if t.Progress != nil {
		t.Progress.OnStart(ctx, t)
	}

	// 1. Pre-flight Checks
	if err := t.preflight(logger); err != nil {
		t.notifyError(ctx, err)
		return err
	}

	// 2. Setup Secure Workspace
	tempDir, err := t.setupWorkspace(logger)
	if err != nil {
		t.notifyError(ctx, err)
		return err
	}
	defer func() {
		// Cleanup logic
		if cleanupErr := os.RemoveAll(tempDir); cleanupErr != nil {
			logger.Warnf("Cleanup warning: %v", cleanupErr)
		}
	}()

	// 3. Concurrency Control
	if err := acquireDownloadSlot(ctx); err != nil {
		logger.Errorf("Traffic Jam: %v", err)
		t.notifyError(ctx, err)
		return err
	}
	defer releaseDownloadSlot()

	// 4. THE DOWNLOAD ENGINE
	downloadedFiles, err := t.downloadWithSmartRetry(ctx, tempDir, logger)
	if err != nil {
		t.notifyError(ctx, err)
		return err
	}

	if len(downloadedFiles) == 0 {
		err := errors.New("download completed but yielded no valid artifacts")
		logger.Error(err.Error())
		t.notifyError(ctx, err)
		return err
	}

	// 5. Post-Download Pipelines
	logger.Infof("Processing %d artifacts in parallel pipelines", len(downloadedFiles))
	eg, egCtx := errgroup.WithContext(ctx)

	// Pipeline A: Chat Upload
	if t.UploadToChat {
		eg.Go(func() error {
			if err := t.uploadFilesToChat(egCtx, downloadedFiles, tempDir); err != nil {
				return fmt.Errorf("chat upload failed: %w", err)
			}
			return nil
		})
	}

	// Pipeline B: Long-term Storage
	if t.Storage != nil {
		eg.Go(func() error {
			if err := t.processStorageTransfer(egCtx, downloadedFiles); err != nil {
				return fmt.Errorf("storage transfer failed: %w", err)
			}
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		logger.Errorf("Pipeline execution failed: %v", err)
		t.notifyError(ctx, err)
		return err
	}

	logger.Infof("✅ Task %s completed successfully.", t.ID)
	if t.Progress != nil {
		t.Progress.OnDone(ctx, t, nil)
	}

	return nil
}

func (t *Task) preflight(logger *log.Logger) error {
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		return errors.New("CRITICAL: yt-dlp binary missing")
	}

	basePath := config.C().Temp.BasePath
	if basePath == "" {
		basePath = os.TempDir()
	}

	free, err := getFreeDiskSpace(basePath)
	if err != nil {
		logger.Warnf("Disk check skipped: %v", err)
	} else if free < minDiskSpace {
		return fmt.Errorf("insufficient disk space in %s: available %s, need %s",
			basePath, dlutil.FormatSize(free), dlutil.FormatSize(minDiskSpace))
	}
	return nil
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

	for attempt := 1; attempt <= maxRetries; attempt++ {
		logger.Infof("Attempt %d/%d: Executing native strategy...", attempt, maxRetries)

		_ = os.MkdirAll(plan.cacheDir, 0o755)

		args := plan.argsForAttempt(attempt, lastClass)
		cmdCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
		res, cmdErr := baseCmd.Run(cmdCtx, args...)
		cancel()

		if cmdErr == nil {
			files, err := t.scanDownloadedFiles(tempDir, logger)
			if err != nil {
				return nil, fmt.Errorf("scan failed after success: %w", err)
			}
			return files, nil
		}

		lastErr = cmdErr
		if errors.Is(cmdErr, context.Canceled) {
			return nil, cmdErr
		}

		if isFatalError(res, cmdErr) {
			logger.Error("Fatal error detected. Aborting.")
			return nil, cmdErr
		}

		failKind, hint := classifyFailure(res, cmdErr)
		lastClass = failKind

		if hint != "" {
			logger.Warnf("Analysis: %s", hint)
		}

		if failKind == failAuthRequired {
			return nil, fmt.Errorf("auth required: %w", cmdErr)
		}

		delay := calculateBackoff(attempt)
		if failKind == failRateLimited {
			delay = delay * 2
			logger.Warn("Rate limit hit. Cooling down...")
		}

		logger.Warnf("Retrying in %s...", delay)
		if err := sleepWithContext(ctx, delay); err != nil {
			return nil, err
		}
	}

	return nil, fmt.Errorf("exhausted %d attempts. Last error: %v", maxRetries, lastErr)
}

type downloadPlan struct {
	baseArgs []string
	userArgs []string
	urls     []string
	cacheDir string
}

func (t *Task) buildDownloadPlan(logger *log.Logger, tempDir string) (*downloadPlan, error) {
	safeFlags, err := sanitizeFlags(t.Flags)
	if err != nil {
		return nil, err
	}

	cacheDir := filepath.Join(tempDir, "cache")

	// --- THE NATIVE WOOSH CONFIGURATION ---
	base := []string{
		"--ignore-config",
		"--no-playlist",
		"--no-mtime",
		"--geo-bypass",
		"--cache-dir", cacheDir,
		"--write-info-json",

		// Resilience
		"--retries", "infinite", // Keep trying connection errors
		"--fragment-retries", hlsMaxFragmentRetries,
		"--file-access-retries", "10",
		"--extractor-retries", "5",

		// Network Optimization (No Aria2 needed)
		"--concurrent-fragments", hlsConcurrentFragments, // The Secret Sauce for HLS
		"--resize-buffer",                  // Dynamic buffer resizing
		"--http-chunk-size", httpChunkSize, // Keep throughput high
		"--buffer-size", "16k", // Optimized disk buffer

		// Protocol Preference (Force Native for speed, FFmpeg for merge)
		"--hls-prefer-native",
		"--downloader", "http,hls,dash:native", // Ensure we use internal engine

		// Filename Hygiene
		"--windows-filenames",
		"--trim-filenames", "200",
		"--restrict-filenames",

		// Metadata
		"--add-metadata",
		"--embed-chapters",
		"--embed-subs",
		"--sub-langs", "en.*,id.*,en,id",

		// User Agent
		"--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	}

	// --- STRICT MP4 ENFORCER ---
	// Logic:
	// 1. If user didn't specify format, we force MP4.
	// 2. We use "--merge-output-format mp4" which remuxes post-download.
	// 3. We add "--recode-video mp4" as a safety net if the merge fails or source is weird.
	if !hasAnyFormatFlag(safeFlags) {
		base = append(base,
			"-f", "bv*[ext=mp4]+ba[ext=m4a]/b[ext=mp4] / bv*+ba/b",
			"--merge-output-format", "mp4",
			"--recode-video", "mp4",
		)
	}

	if t.Cookie != "" {
		if _, err := os.Stat(t.Cookie); err == nil {
			base = append(base, "--cookies", t.Cookie)
		} else {
			logger.Warn("Cookie file specified but missing.")
		}
	}

	if t.UploadToChat {
		base = append(base, "--write-thumbnail", "--convert-thumbnails", "jpg", "--embed-thumbnail")
	} else {
		base = append(base, "--embed-thumbnail")
	}

	return &downloadPlan{
		baseArgs: base,
		userArgs: safeFlags,
		urls:     t.URLs,
		cacheDir: cacheDir,
	}, nil
}

func (p *downloadPlan) argsForAttempt(attempt int, last failureClass) []string {
	args := make([]string, 0, len(p.baseArgs)+len(p.userArgs)+len(p.urls)+32)
	args = append(args, p.baseArgs...)
	args = append(args, p.userArgs...)

	// --- ADAPTIVE NATIVE STRATEGY ---

	switch {
	case attempt >= 2 && (last == failSignatureNsig || last == failUnknown):
		// Switch client if default fails
		args = append(args, "--extractor-args", "youtube:player_client=android,web,ios")

	case last == failRateLimited:
		// Go into stealth mode
		args = append(args,
			"--sleep-requests", "2",
			"--sleep-interval", "5",
			"--max-sleep-interval", "15",
			"--force-ipv4",
		)

	case last == failPostProcess:
		// Save the raw file if FFmpeg fails to merge/embed
		args = append(args, "--no-embed-subs", "--no-embed-thumbnail", "--no-embed-chapters")

	case last == failTransientNetwork && attempt >= 2:
		// If aggressive concurrency is choking the network, throttle back slightly
		// This overwrites the previous "--concurrent-fragments" flag
		args = append(args, "--concurrent-fragments", "4", "--force-ipv4")
	}

	args = append(args, p.urls...)
	return args
}

// --- Failure Classification ---

type failureClass int

const (
	failUnknown failureClass = iota
	failAuthRequired
	failRateLimited
	failSignatureNsig
	failPostProcess
	failTransientNetwork
	failGeoBlock
)

func classifyFailure(res *ytdlp.Result, err error) (failureClass, string) {
	if err == nil {
		return failUnknown, ""
	}
	msg := strings.ToLower(errorMessage(res, err))

	switch {
	case strings.Contains(msg, "sign in") || strings.Contains(msg, "cookies required"):
		return failAuthRequired, "Auth Required"
	case strings.Contains(msg, "429") || strings.Contains(msg, "too many requests"):
		return failRateLimited, "Rate Limited"
	case strings.Contains(msg, "nsig") || strings.Contains(msg, "signature"):
		return failSignatureNsig, "Signature Error"
	case strings.Contains(msg, "postprocessing") || strings.Contains(msg, "ffmpeg"):
		return failPostProcess, "Post-process Error"
	case strings.Contains(msg, "timed out") || strings.Contains(msg, "reset") || strings.Contains(msg, "eof"):
		return failTransientNetwork, "Network Error"
	case strings.Contains(msg, "unavailable in your country") || strings.Contains(msg, "geo"):
		return failGeoBlock, "Geo-blocked"
	default:
		return failUnknown, ""
	}
}

// --- Helpers ---

func (t *Task) scanDownloadedFiles(dir string, logger *log.Logger) ([]string, error) {
	var files []string
	var metadataFiles []string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			if filepath.Base(path) == "cache" {
				return filepath.SkipDir
			}
			return nil
		}

		name := info.Name()
		if strings.HasSuffix(name, ".info.json") {
			metadataFiles = append(metadataFiles, path)
			return nil
		}

		if isIgnoredDownloadArtifact(name) {
			return nil
		}
		if info.Size() == 0 {
			logger.Warnf("Ignoring empty file: %s", name)
			return nil
		}

		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(files)

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
	tempDir, err := os.MkdirTemp(basePath, fmt.Sprintf("ytdlp-native-%s-", t.ID))
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	logger.Debugf("Workspace: %s", tempDir)
	return tempDir, nil
}

func (t *Task) notifyError(ctx context.Context, err error) {
	if t.Progress != nil {
		t.Progress.OnDone(ctx, t, err)
	}
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
		return fmt.Errorf("save failed: %w", err)
	}
	if t.Progress != nil {
		t.Progress.OnProgress(ctx, t, fmt.Sprintf("Transferred: %s", fileName))
	}
	return nil
}

// --- Utils ---

func sanitizeFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == ' ' || r == '.' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name)
	return strings.Trim(name, " ._")
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
		"--external-downloader": true, "--external-downloader-args": true, // We control this strictly
		"--downloader": true, "--downloader-args": true,
		"--ffmpeg-location": true,
	}

	safe := make([]string, 0, len(flags))
	for _, flag := range flags {
		clean := strings.TrimSpace(flag)
		if clean == "" {
			continue
		}
		clean = normalizeFlagToken(clean)
		if prefix := forbiddenFlagPrefix(clean); prefix != "" {
			return nil, fmt.Errorf("forbidden flag prefix: %q", prefix)
		}
		flagName := clean
		if strings.Contains(clean, "=") {
			parts := strings.SplitN(clean, "=", 2)
			flagName = strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			value = stripMatchingQuotes(value)
			clean = fmt.Sprintf("%s=%s", flagName, value)
		}
		if forbidden[flagName] {
			return nil, fmt.Errorf("forbidden flag: %q", flagName)
		}
		safe = append(safe, clean)
	}
	return safe, nil
}

func normalizeFlagToken(token string) string {
	return stripMatchingQuotes(strings.TrimSpace(token))
}

func stripMatchingQuotes(value string) string {
	if len(value) < 2 {
		return value
	}
	first, last := value[0], value[len(value)-1]
	if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
		return value[1 : len(value)-1]
	}
	return value
}

func forbiddenFlagPrefix(flag string) string {
	prefixes := []string{"-o", "-P", "--paths", "--cache-dir", "--output"}
	for _, prefix := range prefixes {
		if flag == prefix || strings.HasPrefix(flag, prefix) {
			return prefix
		}
	}
	return ""
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

func isIgnoredDownloadArtifact(name string) bool {
	lowerName := strings.ToLower(name)
	ignoredSuffixes := []string{
		".part", ".ytdl", ".tmp", ".temp", ".lock",
		".info.json", ".description", ".annotations.xml",
		".vtt", ".srt", ".ass", ".lrc",
		".webp", ".jpg", ".jpeg", ".png",
	}
	for _, suffix := range ignoredSuffixes {
		if strings.HasSuffix(lowerName, suffix) {
			return true
		}
	}
	return false
}

func errorMessage(res *ytdlp.Result, err error) string {
	if err == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(err.Error())
	if res != nil {
		if res.Stdout != "" {
			b.WriteString("\nSTDOUT: " + res.Stdout)
		}
		if res.Stderr != "" {
			b.WriteString("\nSTDERR: " + res.Stderr)
		}
	}
	return b.String()
}

func isFatalError(res *ytdlp.Result, err error) bool {
	msg := strings.ToLower(errorMessage(res, err))
	fatalKeywords := []string{
		"unsupported url", "login required", "account terminated",
		"video unavailable", "private video", "copyright",
		"inappropriate content", "geo-restricted",
	}
	for _, keyword := range fatalKeywords {
		if strings.Contains(msg, keyword) {
			return true
		}
	}
	return false
}

// JSON Metadata parsing
type ytDlpRequestedDownload struct {
	Filepath string `json:"filepath"`
	Filename string `json:"filename"`
}

type ytDlpMetadata struct {
	Filename           string                   `json:"_filename"`
	RequestedDownloads []ytDlpRequestedDownload `json:"requested_downloads"`
}

func filesFromMetadata(metadataFiles []string, logger *log.Logger) []string {
	tracked := make(map[string]struct{})
	for _, metaPath := range metadataFiles {
		content, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var metadata ytDlpMetadata
		if err := json.Unmarshal(content, &metadata); err != nil {
			continue
		}
		addTrackedPath(tracked, resolveMetadataPath(metaPath, metadata.Filename), logger)
		for _, requested := range metadata.RequestedDownloads {
			path := requested.Filepath
			if path == "" {
				path = requested.Filename
			}
			addTrackedPath(tracked, resolveMetadataPath(metaPath, path), logger)
		}
	}
	if len(tracked) == 0 {
		return nil
	}
	files := make([]string, 0, len(tracked))
	for path := range tracked {
		files = append(files, path)
	}
	sort.Strings(files)
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
	if err != nil || info.Size() == 0 {
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
	if runtime.GOOS == "windows" {
		return math.MaxInt64, nil
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

func formatProgressStatus(update ytdlp.ProgressUpdate) string {
	action := "Processing"
	switch update.Status {
	case ytdlp.ProgressStatusStarting:
		action = "Warm-up"
	case ytdlp.ProgressStatusDownloading:
		action = "Downloading"
	case ytdlp.ProgressStatusPostProcessing:
		action = "Remuxing/Fixing"
	case ytdlp.ProgressStatusFinished:
		action = "Done"
	}

	if update.Status == ytdlp.ProgressStatusDownloading {
		percent := update.Percent()
		bar := buildProgressBar(percent, 10)
		speed := formatSpeed(update)
		if speed != "" {
			return fmt.Sprintf("%s %s %s [%s]", action, bar, update.PercentString(), speed)
		}
		return fmt.Sprintf("%s %s %s", action, bar, update.PercentString())
	}
	return action
}

func buildProgressBar(percent float64, width int) string {
	if width <= 0 {
		width = 10
	}
	if percent < 0 {
		percent = 0
	} else if percent > 100 {
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
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
