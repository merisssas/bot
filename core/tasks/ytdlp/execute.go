package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	ytdlp "github.com/lrstanley/go-ytdlp"
	"golang.org/x/sync/errgroup"

	"github.com/merisssas/bot/common/utils/dlutil"
	"github.com/merisssas/bot/config"
	"github.com/merisssas/bot/pkg/enums/ctxkey"
)

const (
	baseDelay    = 1 * time.Second
	maxDelay     = 30 * time.Second
	maxRetries   = 5
	multiplier   = 2.0
	jitterFactor = 0.2
)

// Execute implements core.Executable with resilient processing.
func (t *Task) Execute(ctx context.Context) error {
	logger := log.FromContext(ctx).With("task_id", t.ID)
	logger.Infof("Starting yt-dlp task")

	if t.Progress != nil {
		t.Progress.OnStart(ctx, t)
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

func (t *Task) downloadWithSmartRetry(ctx context.Context, tempDir string, logger *log.Logger) ([]string, error) {
	args, err := t.buildArguments(ctx, logger)
	if err != nil {
		return nil, err
	}

	baseCmd := ytdlp.New().
		SetSeparateProcessGroup(true).
		Output(filepath.Join(tempDir, "%(title)s.%(ext)s"))

	if t.Progress != nil {
		baseCmd = baseCmd.ProgressFunc(500*time.Millisecond, func(update ytdlp.ProgressUpdate) {
			status := formatProgressStatus(update)
			if status != "" {
				t.Progress.OnProgress(ctx, t, status)
			}
		})
	}

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		logger.Infof("Attempt %d/%d: executing download strategy", attempt, maxRetries)
		_, cmdErr := baseCmd.Run(ctx, args...)
		if cmdErr == nil {
			return t.scanDownloadedFiles(tempDir, logger)
		}

		lastErr = cmdErr
		if errors.Is(cmdErr, context.Canceled) {
			return nil, cmdErr
		}

		delay := calculateBackoff(attempt)
		logger.Warnf("Download failed: %v. Retrying in %s...", cmdErr, delay)
		if err := sleepWithContext(ctx, delay); err != nil {
			return nil, err
		}
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

func (t *Task) buildArguments(ctx context.Context, logger *log.Logger) ([]string, error) {
	safeFlags, err := sanitizeFlags(t.Flags)
	if err != nil {
		return nil, err
	}

	args := []string{
		"--no-mtime",
		"--abort-on-error",
		"--no-playlist",
		"--geo-bypass",
		"--ignore-errors",
		"--concurrent-fragments", "4",
		"--add-metadata",
		"--embed-thumbnail",
		"--embed-chapters",
		"--embed-subs",
		"--sub-langs", "en,id,all",
		"--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	}

	userHasFormat := false
	for _, flag := range safeFlags {
		if strings.Contains(flag, "-f") || strings.Contains(flag, "--format") {
			userHasFormat = true
			break
		}
	}
	if !userHasFormat {
		args = append(args, "-f", "bv*[ext=mp4]+ba[ext=m4a]/b[ext=mp4] / bv*+ba/b")
		args = append(args, "--merge-output-format", "mp4")
	}

	if t.Cookie != "" {
		if _, err := os.Stat(t.Cookie); err == nil {
			args = append(args, "--cookies", t.Cookie)
		} else {
			logger.Warn("Cookie file specified but not found, proceeding without it")
		}
	}

	if t.UploadToChat {
		args = append(args, "--write-thumbnail")
		args = append(args, "--convert-thumbnails", "jpg")
	}

	args = appendProxyArgs(ctx, logger, args, t.URLs, safeFlags)
	args = append(args, safeFlags...)
	args = append(args, t.URLs...)
	return args, nil
}

func (t *Task) scanDownloadedFiles(dir string, logger *log.Logger) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if isIgnoredDownloadArtifact(info.Name()) {
			logger.Debugf("Skipping non-primary download artifact: %s", info.Name())
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, err
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
		t.Progress.OnProgress(ctx, t, fmt.Sprintf("Archived: %s", fileName))
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
	backoff := float64(baseDelay) * math.Pow(multiplier, float64(attempt))
	if backoff > float64(maxDelay) {
		backoff = float64(maxDelay)
	}

	random := rand.Float64()*(jitterFactor*2) + (1 - jitterFactor)
	return time.Duration(backoff * random)
}

func isIgnoredDownloadArtifact(name string) bool {
	lowerName := strings.ToLower(name)
	ignoredSuffixes := []string{
		".part",
		".ytdl",
		".tmp",
		".temp",
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
	return fmt.Sprintf("%s%s", strings.Repeat("▓", filled), strings.Repeat("░", width-filled))
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
