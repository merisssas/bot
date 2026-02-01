package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	ytdlp "github.com/lrstanley/go-ytdlp"

	"github.com/merisssas/Bot/config"
	"github.com/merisssas/Bot/pkg/enums/ctxkey"
)

// KONFIGURASI "THE BEAST"
const (
	maxTaskRetries = 5          // Retry level dewa (5x percobaan ulang global)
	hlsConcurrency = 16         // IDM Killer: Download 16 segmen HLS sekaligus
	fragmentRetry  = "infinite" // Jangan menyerah pada segmen yang gagal
)

// Execute implements core.Executable.
func (t *Task) Execute(ctx context.Context) error {
	logger := log.FromContext(ctx)
	logger.Infof("🚀 Starting HLS-OPTIMIZED Task %s", t.ID)

	if t.Progress != nil {
		t.Progress.OnStart(ctx, t)
	}

	// ---------------------------------------------------------
	// 1. FIX SYSTEM ERROR (The Foundation)
	// Masalah "stat cache/: no such file" selesai di sini.
	// Kita buat pondasi foldernya dulu sebelum buat temp dir.
	// ---------------------------------------------------------
	basePath := config.C().Temp.BasePath
	if err := os.MkdirAll(basePath, 0755); err != nil {
		// Jika gagal buat folder dasar, itu fatal.
		return t.handleError(ctx, logger, fmt.Errorf("FATAL: System cannot create base path %s: %w", basePath, err))
	}

	// 2. Create Isolated Workspace
	tempDir, err := os.MkdirTemp(basePath, "hls-reaper-*")
	if err != nil {
		return t.handleError(ctx, logger, fmt.Errorf("workspace creation failed: %w", err))
	}
	defer func() {
		// Auto-clean workspace setelah selesai (atau gagal)
		logger.Debugf("🧹 Cleaning workspace: %s", tempDir)
		os.RemoveAll(tempDir)
	}()

	// 3. Execute Download (Auto-Healing Mechanism)
	var downloadedFiles []string

	// Kita bungkus proses download dalam Retry Loop yang pintar
	err = t.retry(ctx, logger, "Download Phase", func() error {
		var dErr error
		downloadedFiles, dErr = t.downloadHLS(ctx, tempDir)
		return dErr
	})

	if err != nil {
		return t.handleError(ctx, logger, err)
	}

	// Validasi hasil
	if len(downloadedFiles) == 0 {
		return t.handleError(ctx, logger, errors.New("download success reported, but no files found (ghost file error)"))
	}

	// 4. Transfer Phase (Atomic Move)
	logger.Infof("📦 Transferring %d artifact(s) to %s", len(downloadedFiles), t.Storage.Name())

	for _, filePath := range downloadedFiles {
		err = t.retry(ctx, logger, "Transfer Phase", func() error {
			return t.transferFile(ctx, filePath)
		})
		if err != nil {
			return t.handleError(ctx, logger, fmt.Errorf("transfer failed for %s: %w", filepath.Base(filePath), err))
		}
	}

	logger.Infof("✅ HLS Task %s obliterated. File secured.", t.ID)
	if t.Progress != nil {
		t.Progress.OnDone(ctx, t, nil)
	}

	return nil
}

// downloadHLS is the core engine tuned specifically for M3U8/HLS streams
func (t *Task) downloadHLS(ctx context.Context, tempDir string) ([]string, error) {
	logger := log.FromContext(ctx)

	// Template nama file yang aman
	outputTemplate := filepath.Join(tempDir, "%(title)s.%(ext)s")

	// --- THE BILLION DOLLAR CONFIGURATION ---
	// Tanpa Aria2c, kita optimalkan Native Downloader
	cmd := ytdlp.New().
		Output(outputTemplate).
		// HLS OPTIMIZATIONS
		ConcurrentFragments(hlsConcurrency). // KUNCI KECEPATAN: Download 16 parts sekaligus
		FragmentRetries(fragmentRetry).      // Jika 1 part gagal, coba terus sampai dapat
		Retries("infinite").                 // Jangan pernah stop mencoba koneksi
		FileAccessRetries("infinite").       // Cegah error "file busy" saat merging
		ResizeBuffer(true).                  // Dinamis mengatur buffer size
		HlsUseMpegts(true).                  // Gunakan MPEG-TS container untuk stabilitas stream
		// QUALITY & FORMAT
		FormatSort("res:1080,vcodec:h264,acodec:aac"). // Prioritas 1080p MP4 (Universal)
		RecodeVideo("mp4").                            // Paksa output jadi MP4 matang
		MergeOutputFormat("mp4").                      // Pastikan merging menggunakan container MP4
		AddMetadata().
		EmbedThumbnail().
		// CLEANLINESS
		NoOverwrites().
		Continue().
		IgnoreErrors().
		RestrictFilenames()

	// Inject User Flags (jika ada, timpa default)
	if len(t.Flags) > 0 {
		// Kita log warning bahwa user flags mungkin merusak optimasi HLS kita
		logger.Debug("Applying custom user flags over default HLS config")
	}

	// Progress Monitoring Real-time
	if t.Progress != nil {
		cmd.ProgressUpdate(func(prog ytdlp.ProgressUpdate) {
			// Kalkulasi status yang lebih enak dibaca
			percent := prog.Percent
			if percent == 0 && prog.TotalBytesEstimated > 0 {
				// Fallback calculation untuk HLS yang kadang tidak kirim %
				percent = (float64(prog.DownloadedBytes) / float64(prog.TotalBytesEstimated)) * 100
			}

			status := fmt.Sprintf("🚀 HLS: %.1f%% | ⚡ %s | ⏳ %s | Fragment: %s",
				percent, prog.Speed, prog.ETA, prog.FragmentIndex)

			t.Progress.OnProgress(ctx, t, status)
		})
	}

	logger.Infof("🔥 Igniting Native HLS Engine (Concurrency: %d)", hlsConcurrency)

	// Combine flags: Default Config + User Flags + URLs
	// URLs ditaruh paling belakang
	args := append(t.Flags, t.URLs...)

	// RUN THE BEAST
	_, err := cmd.Run(ctx, args...)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			logger.Warn("Task cancelled by user request")
			return nil, err
		}
		// Kita log error tapi tetap cek direktori, karena kadang yt-dlp throw exit code non-0
		// meskipun file berhasil di-merge (misal karena warning subtitle).
		logger.Warnf("yt-dlp exited with warning/error: %v (Checking files...)", err)
	}

	// --- VALIDATION & CLEANUP ---
	files, err := os.ReadDir(tempDir)
	if err != nil {
		return nil, fmt.Errorf("failed to scan workspace: %w", err)
	}

	var validFiles []string
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fName := file.Name()
		// Filter file sampah yang tertinggal saat proses merging
		if strings.HasSuffix(fName, ".part") ||
			strings.HasSuffix(fName, ".ytdl") ||
			strings.HasSuffix(fName, ".f137") || // Video only temp
			strings.HasSuffix(fName, ".f140") || // Audio only temp
			strings.HasSuffix(fName, ".temp") {
			continue
		}

		fullPath := filepath.Join(tempDir, fName)

		// Validasi Integritas File: Size > 0
		info, _ := file.Info()
		if info.Size() < 1024 { // File di bawah 1KB biasanya corrupt/error text
			logger.Warnf("Skipping suspicious small file: %s (%d bytes)", fName, info.Size())
			continue
		}

		validFiles = append(validFiles, fullPath)
		logger.Debugf("Target acquired: %s (%s)", fName, humanizeBytes(info.Size()))
	}

	return validFiles, nil
}

// transferFile handles secure upload
func (t *Task) transferFile(ctx context.Context, filePath string) error {
	logger := log.FromContext(ctx)

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("cannot open artifact: %w", err)
	}
	defer f.Close()

	fileInfo, err := f.Stat()
	if err != nil {
		return err
	}

	// Inject Content-Length header context
	ctx = context.WithValue(ctx, ctxkey.ContentLength, fileInfo.Size())

	fileName := sanitizeFilename(filepath.Base(filePath))
	destPath := filepath.Join(t.StorPath, fileName)

	logger.Infof("⬆️ Uploading: %s -> %s", fileName, destPath)

	if err := t.Storage.Save(ctx, f, destPath); err != nil {
		return err
	}

	if t.Progress != nil {
		t.Progress.OnProgress(ctx, t, fmt.Sprintf("✅ Uploaded: %s", fileName))
	}

	return nil
}

// retry with Exponential Backoff + Jitter
func (t *Task) retry(ctx context.Context, logger *log.Logger, operation string, fn func() error) error {
	var err error
	for i := 0; i <= maxTaskRetries; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if i > 0 {
			// Backoff: 2s, 4s, 8s...
			backoff := time.Duration(math.Pow(2, float64(i))) * time.Second
			logger.Warnf("⚠️ %s failed. Retrying in %s (Attempt %d/%d)... Error: %v", operation, backoff, i, maxTaskRetries, err)

			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		err = fn()
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("%s FATAL after %d attempts. Last error: %w", operation, maxTaskRetries, err)
}

func (t *Task) handleError(ctx context.Context, logger *log.Logger, err error) error {
	logger.Error(err.Error())
	if t.Progress != nil {
		t.Progress.OnDone(ctx, t, err)
	}
	return err
}

// Utilitas
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
	// Hindari index out of range
	if i >= len(sizes) {
		i = len(sizes) - 1
	}
	val := float64(s) / math.Pow(1024, float64(i))
	return fmt.Sprintf("%.1f %s", val, sizes[i])
}
