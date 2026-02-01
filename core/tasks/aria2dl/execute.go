package aria2dl

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"golang.org/x/sync/errgroup"

	"github.com/merisssas/bot/common/utils/dlutil"
	"github.com/merisssas/bot/config"
	"github.com/merisssas/bot/pkg/aria2"
	"github.com/merisssas/bot/pkg/enums/ctxkey"
)

// Constants for tuning.
const (
	minPollInterval   = 1 * time.Second
	maxPollInterval   = 5 * time.Second
	diskCheckInterval = 30 * time.Second
)

// Execute implements core.Executable with Bulletproof Orchestration.
func (t *Task) Execute(ctx context.Context) (err error) {
	logger := log.FromContext(ctx).With("task_id", t.ID, "gid", t.GID)
	logger.Info("🚀 Starting Ultimate Aria2 Task")

	if err := t.validateDependencies(); err != nil {
		return err
	}

	if t.Progress != nil {
		t.Progress.OnStart(ctx, t)
	}

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic recovered in execute: %v", r)
			logger.Error("🔥 Critical Panic Recovered", "error", err)
			if t.Progress != nil {
				t.Progress.OnDone(ctx, t, err)
			}
		}
	}()

	completedGIDs, downloadErr := t.runDownloadSequence(ctx, logger)

	cleanupCtx := context.WithoutCancel(ctx)
	defer t.cleanupDownloadResults(cleanupCtx, completedGIDs)

	if downloadErr != nil {
		if errors.Is(downloadErr, context.Canceled) {
			logger.Warn("Task canceled by user, initiating graceful stop...")
			t.cancelAria2Download(cleanupCtx)
		} else {
			logger.Error("Download sequence failed", "error", downloadErr)
		}

		if t.Progress != nil {
			t.Progress.OnDone(ctx, t, downloadErr)
		}
		return downloadErr
	}

	logger.Info("📦 Download complete, starting transfer pipeline...")
	if err := t.transferPipeline(ctx, completedGIDs); err != nil {
		logger.Error("Transfer pipeline failed", "error", err)
		if t.Progress != nil {
			t.Progress.OnDone(ctx, t, err)
		}
		return err
	}

	logger.Info("✅ Task executed successfully")
	if t.Progress != nil {
		t.Progress.OnDone(ctx, t, nil)
	}

	return nil
}

// runDownloadSequence manages the lifecycle of the download, including BitTorrent metadata follow-ups.
func (t *Task) runDownloadSequence(ctx context.Context, logger *log.Logger) ([]string, error) {
	var completed []string
	currentGID := t.GID
	seen := map[string]bool{currentGID: true}
	queue := []string{currentGID}

	for len(queue) > 0 {
		targetGID := queue[0]
		queue = queue[1:]

		logger.Infof("📡 Monitoring GID: %s", targetGID)

		t.GID = targetGID

		status, err := t.monitorDownloadLoop(ctx, targetGID)
		if err != nil {
			return completed, err
		}

		completed = append(completed, targetGID)

		if len(status.FollowedBy) > 0 {
			logger.Infof("🔗 GID %s spawned new tasks: %v", targetGID, status.FollowedBy)
			for _, newGID := range status.FollowedBy {
				if newGID != "" && !seen[newGID] {
					seen[newGID] = true
					queue = append(queue, newGID)
				}
			}
		}
	}

	return completed, nil
}

// monitorDownloadLoop is the heart of the downloader with Adaptive Polling.
func (t *Task) monitorDownloadLoop(ctx context.Context, gid string) (*aria2.Status, error) {
	logger := log.FromContext(ctx)

	interval := minPollInterval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	lastProgress := time.Now()
	var lastBytes int64

	diskCheckTicker := time.NewTicker(diskCheckInterval)
	defer diskCheckTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-diskCheckTicker.C:
			status, err := t.getStatus(ctx, gid)
			if err == nil {
				if _, err := t.ensureDiskSpace(ctx, status); err != nil {
					return nil, err
				}
			}
		case <-ticker.C:
			status, err := t.getStatus(ctx, gid)
			if err != nil {
				logger.Warn("Transient status check error", "error", err)
				continue
			}

			if t.Progress != nil {
				t.Progress.OnProgress(ctx, t, status)
			}

			currBytes, _ := parseLength(status.CompletedLength)
			if currBytes > lastBytes {
				lastBytes = currBytes
				lastProgress = time.Now()
				if interval > minPollInterval {
					interval = minPollInterval
					ticker.Reset(interval)
				}
			} else {
				if time.Since(lastProgress) > 10*time.Second && interval < maxPollInterval {
					interval = maxPollInterval
					ticker.Reset(interval)
				}
			}

			if t.idleTimeout > 0 && time.Since(lastProgress) > t.idleTimeout {
				return nil, fmt.Errorf("stalled: no progress for %s", t.idleTimeout)
			}

			if status.IsDownloadComplete() {
				if status.VerifyIntegrityPending == "true" {
					continue
				}
				if err := validateIntegrity(status); err != nil {
					return nil, err
				}
				return status, nil
			}

			if status.IsDownloadError() {
				return nil, fmt.Errorf("aria2 error %s: %s", status.ErrorCode, status.ErrorMessage)
			}
			if status.IsDownloadRemoved() {
				return nil, errors.New("download removed externally")
			}
		}
	}
}

// transferPipeline orchestrates high-performance parallel transfers using ErrGroup.
func (t *Task) transferPipeline(ctx context.Context, gids []string) error {
	logger := log.FromContext(ctx)

	var workItems []transferWork
	destinations := make(map[string]string)
	var localFiles []string

	for _, gid := range gids {
		status, err := t.Aria2Client.TellStatus(ctx, gid)
		if err != nil {
			return fmt.Errorf("status fetch failed during transfer: %w", err)
		}

		for _, file := range status.Files {
			if file.Selected != "true" || filepath.Ext(file.Path) == ".torrent" {
				if filepath.Ext(file.Path) == ".torrent" {
					t.removeFileIfNeeded(ctx, file.Path)
				}
				continue
			}

			localFiles = append(localFiles, file.Path)
			destPath := t.resolveSmartDestination(status.Dir, file, gid)

			if existing, ok := destinations[destPath]; ok {
				return fmt.Errorf("collision detected: %s and %s map to same dest %s", existing, file.Path, destPath)
			}
			destinations[destPath] = file.Path
			workItems = append(workItems, transferWork{source: file.Path, destination: destPath})
		}
	}

	if len(workItems) == 0 {
		return errors.New("no downloadable files found in task")
	}

	workerCount := config.C().Workers
	if workerCount < 1 {
		workerCount = 4
	}

	eg, gctx := errgroup.WithContext(ctx)
	eg.SetLimit(workerCount)

	logger.Infof("🚀 Transferring %d files with %d workers", len(workItems), workerCount)

	for _, work := range workItems {
		work := work
		eg.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}

			if err := t.transferAtomic(gctx, work.source, work.destination); err != nil {
				return err
			}

			t.removeFileIfNeeded(context.WithoutCancel(gctx), work.source)
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		logger.Error("Transfer pipeline partially failed", "error", err)
		t.cleanupLocalFiles(context.WithoutCancel(ctx), localFiles)
		return err
	}

	return nil
}

// transferAtomic handles a single file transfer with robust checks.
func (t *Task) transferAtomic(ctx context.Context, src, dst string) error {
	logger := log.FromContext(ctx)

	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("source missing: %w", err)
	}

	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	ctx = context.WithValue(ctx, ctxkey.ContentLength, info.Size())

	logger.Debugf("Transmitting: %s -> %s (%s)", filepath.Base(src), dst, dlutil.FormatSize(info.Size()))

	if err := t.Storage.Save(ctx, f, dst); err != nil {
		return fmt.Errorf("storage save failed: %w", err)
	}
	return nil
}

// resolveSmartDestination determines the best path, handling edge cases better.
func (t *Task) resolveSmartDestination(baseDir string, file aria2.File, gid string) string {
	relPath := file.Path
	if baseDir != "" {
		if rel, err := filepath.Rel(baseDir, file.Path); err == nil {
			relPath = rel
		}
	}

	cleaned := sanitizeRelativePath(relPath)

	if cleaned == "" || cleaned == "." {
		cleaned = t.fallbackFileName(file, gid)
	}

	return filepath.Join(t.StorPath, cleaned)
}

// validateDependencies checks required fields before starting.
func (t *Task) validateDependencies() error {
	if t.Aria2Client == nil {
		return errors.New("internal error: aria2 client missing")
	}
	if t.Storage == nil {
		return errors.New("internal error: storage backend missing")
	}
	if t.GID == "" {
		return errors.New("internal error: invalid GID")
	}
	if t.StorPath == "" {
		t.StorPath = "."
	}
	return nil
}

// getStatus implements a multi-queue check strategy.
func (t *Task) getStatus(ctx context.Context, gid string) (*aria2.Status, error) {
	status, err := t.Aria2Client.TellStatus(ctx, gid)
	if err == nil {
		return status, nil
	}

	if waiting, err := t.findStatusInQueue(ctx, gid, t.Aria2Client.TellWaiting); err == nil && waiting != nil {
		return waiting, nil
	}

	if stopped, err := t.findStatusInQueue(ctx, gid, t.Aria2Client.TellStopped); err == nil && stopped != nil {
		return stopped, nil
	}

	return nil, fmt.Errorf("GID %s not found in any queue", gid)
}

// findStatusInQueue is optimized to not spam logs or fail hard.
func (t *Task) findStatusInQueue(
	ctx context.Context,
	gid string,
	fetch func(context.Context, int, int, ...string) ([]aria2.Status, error),
) (*aria2.Status, error) {
	const batch = 50
	for offset := 0; offset < 200; offset += batch {
		statuses, err := fetch(ctx, offset, batch)
		if err != nil {
			return nil, err
		}

		for _, s := range statuses {
			if s.GID == gid {
				return &s, nil
			}
		}
		if len(statuses) < batch {
			break
		}
	}
	return nil, nil
}

// cancelAria2Download uses a safe timeout context.
func (t *Task) cancelAria2Download(ctx context.Context) {
	if t.Aria2Client == nil || t.GID == "" {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	t.Aria2Client.ForceRemove(ctx, t.GID)
	t.Aria2Client.RemoveDownloadResult(ctx, t.GID)
}

// ensureDiskSpace is cross-platform safe(r).
func (t *Task) ensureDiskSpace(ctx context.Context, status *aria2.Status) (bool, error) {
	totalLen, _ := parseLength(status.TotalLength)
	if totalLen <= 0 {
		return false, nil
	}

	dir := status.Dir
	if dir == "" && len(status.Files) > 0 {
		dir = filepath.Dir(status.Files[0].Path)
	}
	if dir == "" {
		return false, nil
	}

	free, err := getFreeDiskSpace(dir)
	if err != nil {
		log.FromContext(ctx).Warn("Could not check disk space", "dir", dir, "error", err)
		return false, nil
	}

	minFree := config.C().Aria2.MinFreeSpaceMB * 1024 * 1024
	if free < (totalLen + minFree) {
		return true, fmt.Errorf("disk full in %s: need %s, have %s", dir, dlutil.FormatSize(totalLen), dlutil.FormatSize(free))
	}
	return true, nil
}

// getFreeDiskSpace abstracts syscall for basic safety.
func getFreeDiskSpace(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

// --- Helpers ---

type transferWork struct {
	source, destination string
}

func (t *Task) removeFileIfNeeded(ctx context.Context, path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.FromContext(ctx).Debug("Failed to cleanup file", "path", path, "error", err)
	}
}

func (t *Task) cleanupLocalFiles(ctx context.Context, paths []string) {
	for _, p := range paths {
		t.removeFileIfNeeded(ctx, p)
	}
}

func (t *Task) cleanupDownloadResults(ctx context.Context, gids []string) {
	for _, gid := range gids {
		if gid != "" {
			t.Aria2Client.RemoveDownloadResult(ctx, gid)
		}
	}
}

func (t *Task) fallbackFileName(file aria2.File, gid string) string {
	if base := filepath.Base(file.Path); base != "." && base != "/" {
		return sanitizePathSegment(base)
	}
	for _, u := range file.URIs {
		if u.URI != "" {
			if parsed, _ := url.Parse(u.URI); parsed != nil {
				if base := path.Base(parsed.Path); base != "." && base != "/" {
					return sanitizePathSegment(base)
				}
			}
		}
	}
	return fmt.Sprintf("aria2_%s_%s", gid, file.Index)
}

func parseLength(v string) (int64, error) {
	if v == "" {
		return 0, nil
	}
	return strconv.ParseInt(v, 10, 64)
}

func validateIntegrity(s *aria2.Status) error {
	if s.VerifiedLength != "" && s.TotalLength != "" && s.VerifyIntegrityPending == "false" {
		if s.VerifiedLength != s.TotalLength {
			return fmt.Errorf("integrity check mismatch: %s vs %s", s.VerifiedLength, s.TotalLength)
		}
	}
	return nil
}

func sanitizeRelativePath(p string) string {
	clean := filepath.Clean(p)
	if strings.HasPrefix(clean, "..") || strings.HasPrefix(clean, "/") || (len(clean) >= 2 && clean[1] == ':') {
		return sanitizePathSegment(filepath.Base(p))
	}
	return clean
}

func sanitizePathSegment(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_' || r == '.':
			return r
		default:
			return '_'
		}
	}, s)
}
