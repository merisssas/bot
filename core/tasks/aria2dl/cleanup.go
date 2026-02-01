package aria2dl

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/log"

	"github.com/merisssas/bot/config"
	"github.com/merisssas/bot/pkg/aria2"
)

const (
	cleanupBatchSize    = 100
	cleanupSafetyWindow = 2 * time.Minute // File lebih baru dari ini KEBAL terhadap penghapusan
)

// StartupCleanup performs a surgical removal of orphaned aria2 control files.
// It is engineered to be race-condition proof and symlink-safe.
func StartupCleanup(ctx context.Context) {
	logger := log.FromContext(ctx)
	cfg := config.C().Aria2

	if !cfg.Enable || !cfg.StartupCleanup {
		return
	}

	logger.Info("🧹 Starting Aria2 Workspace Cleanup...")

	// 1. Connection Phase
	// Use a short timeout context for initialization to prevent hanging boot
	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client, err := aria2.NewClient(cfg.Url, cfg.Secret)
	if err != nil {
		logger.Warnf("Cleanup skipped: Failed to connect to Aria2: %v", err)
		return
	}

	// 2. Discovery Phase (Directory)
	cleanupDir, err := resolveCleanupDir(initCtx, client, cfg.CleanupDir)
	if err != nil {
		logger.Warnf("Cleanup skipped: Could not resolve directory: %v", err)
		return
	}

	// Canonicalize path to ensure robust comparisons
	absCleanupDir, err := filepath.Abs(cleanupDir)
	if err != nil {
		logger.Warnf("Cleanup skipped: Path resolution failed: %v", err)
		return
	}

	if err := validateDir(absCleanupDir); err != nil {
		logger.Warnf("Cleanup skipped: %v", err)
		return
	}

	// 3. Intelligence Phase (Collect Active/Paused/Stopped Tasks)
	// We must know EVERYTHING Aria2 is holding onto.
	activeFiles, err := collectAllAria2Files(ctx, client)
	if err != nil {
		logger.Warnf("Cleanup aborted: Failed to fetch Aria2 state: %v", err)
		return
	}

	logger.Debugf("Knowledge Base: Aria2 is tracking %d files", len(activeFiles))

	// 4. Execution Phase (The Sweep)
	var (
		removedCount   int64
		reclaimedBytes int64
		skippedCount   int64
	)

	err = filepath.WalkDir(absCleanupDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Perm error? Log and continue scanning other parts
			logger.Debugf("Access denied during walk: %v", walkErr)
			return nil
		}

		// Security: Don't traverse symlinks to avoid escaping jail
		if entry.Type()&fs.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if entry.IsDir() {
			// Optimization: Skip hidden directories like .git if they exist inside
			if strings.HasPrefix(entry.Name(), ".") && entry.Name() != "." {
				return filepath.SkipDir
			}
			return nil
		}

		// Filter Candidates
		if !isCleanupCandidate(entry.Name()) {
			return nil
		}

		// Safety Check 1: Time Immunity
		// If a file was modified recently, it might be a brand new download
		// that hasn't registered in our 'activeFiles' snapshot yet.
		info, err := entry.Info()
		if err == nil && time.Since(info.ModTime()) < cleanupSafetyWindow {
			skippedCount++
			return nil
		}

		// Canonicalize for comparison
		absPath, err := filepath.Abs(path)
		if err != nil {
			return nil
		}

		// Safety Check 2: Active Registry
		if _, isActive := activeFiles[absPath]; isActive {
			return nil
		}

		// Action: Delete
		if err := os.Remove(path); err != nil {
			logger.Warnf("Failed to delete zombie file %s: %v", path, err)
		} else {
			removedCount++
			if info != nil {
				reclaimedBytes += info.Size()
			}
			logger.Debugf("🗑️ Reclaimed: %s", entry.Name())
		}

		return nil
	})

	if err != nil {
		logger.Errorf("Critical error during cleanup walk: %v", err)
	}

	if removedCount > 0 {
		logger.Infof("✨ Hygiene Complete: Removed %d orphaned files, reclaimed %s. (Skipped %d young files)",
			removedCount, formatBytes(reclaimedBytes), skippedCount)
	} else {
		logger.Info("✨ Hygiene Complete: System is clean.")
	}
}

// collectAllAria2Files retrieves Active, Waiting, AND Stopped tasks.
// Including 'Stopped' is crucial because 'Error' or 'Paused' tasks hold valid .aria2 files
// that allow resumption. We must not delete them.
func collectAllAria2Files(ctx context.Context, client *aria2.Client) (map[string]struct{}, error) {
	knownFiles := make(map[string]struct{})

	// helper to process status list
	process := func(statuses []aria2.Status) {
		for _, status := range statuses {
			for _, file := range status.Files {
				if file.Path == "" {
					continue
				}

				// Resolve absolute path
				absPath, err := filepath.Abs(file.Path)
				if err != nil {
					continue
				}

				// Register the file itself
				knownFiles[absPath] = struct{}{}
				// Register the control file (.aria2)
				knownFiles[absPath+".aria2"] = struct{}{}
				// Register the part file (.part) - common in Firefox/Wget but Aria2 might check it
				knownFiles[absPath+".part"] = struct{}{}
			}
		}
	}

	// 1. Active (Downloading)
	active, err := client.TellActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("active: %w", err)
	}
	process(active)

	// 2. Waiting (Queue)
	if err := paginateFetch(ctx, client.TellWaiting, process); err != nil {
		return nil, fmt.Errorf("waiting: %w", err)
	}

	// 3. Stopped (Finished/Error/Paused) -> VITAL for resumption safety
	if err := paginateFetch(ctx, client.TellStopped, process); err != nil {
		return nil, fmt.Errorf("stopped: %w", err)
	}

	return knownFiles, nil
}

// paginateFetch abstracts the offset-based fetching logic
func paginateFetch(
	ctx context.Context,
	fetcher func(context.Context, int, int, ...string) ([]aria2.Status, error),
	processor func([]aria2.Status),
) error {
	for offset := 0; ; offset += cleanupBatchSize {
		items, err := fetcher(ctx, offset, cleanupBatchSize)
		if err != nil {
			return err
		}

		processor(items)

		if len(items) < cleanupBatchSize {
			break
		}
	}
	return nil
}

func resolveCleanupDir(ctx context.Context, client *aria2.Client, configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	// Fallback to Aria2 Global Config
	options, err := client.GetGlobalOption(ctx)
	if err != nil {
		return "", err
	}
	if dir, ok := options["dir"].(string); ok && dir != "" {
		return dir, nil
	}
	return "", fmt.Errorf("aria2 global option 'dir' is not set")
}

func validateDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("directory does not exist: %s", dir)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", dir)
	}
	return nil
}

func isCleanupCandidate(name string) bool {
	lower := strings.ToLower(name)
	// .aria2 = Control file
	// .part = Partial download file (legacy support)
	// .torrent = Only delete if orphaned (implies Metadata download failed)
	return strings.HasSuffix(lower, ".aria2") || strings.HasSuffix(lower, ".part")
}

// Format bytes helper (internal usage to avoid dependency loops if dlutil is heavy)
func formatBytes(b int64) string {
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
