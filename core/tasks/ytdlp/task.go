package ytdlp

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/merisssas/Bot/core"
	"github.com/merisssas/Bot/pkg/enums/tasktype"
	"github.com/merisssas/Bot/storage"
)

// Pastikan Task mengimplementasikan interface core.Executable
var _ core.Executable = (*Task)(nil)

type Task struct {
	// Identity & Telemetry
	ID        string
	CreatedAt time.Time

	// Context Management
	ctx    context.Context
	cancel context.CancelFunc

	// Payload
	URLs  []string
	Flags []string

	// Destinations
	Storage  storage.Storage
	StorPath string

	// Flexible Metadata (Billion Dollar Feature)
	// Mengizinkan penambahan opsi dinamis tanpa merubah struktur struct
	Meta map[string]interface{}

	// Observer
	Progress ProgressTracker

	Config TaskConfig
	Stats  TaskStats

	statsMu sync.Mutex
	pauseMu sync.Mutex
	paused  bool
	pauseCh chan struct{}

	logFile *os.File
}

// NewTask creates a robust, sanitized, and validation-checked Task instance.
// Bertindak sebagai "Firewall" yang memfilter data sampah sebelum masuk ke sistem.
func NewTask(
	id string,
	ctx context.Context,
	urls []string,
	flags []string,
	stor storage.Storage,
	storPath string,
	progressTracker ProgressTracker,
) (*Task, error) {
	// 1. Validation: Storage Critical Check
	// Sistem miliaran dolar tidak boleh panic karena nil pointer.
	// Kita Fail Fast di sini.
	if stor == nil {
		return nil, fmt.Errorf("storage is required")
	}

	// 2. Advanced Sanitization (RFC-Compliant)
	cleanURLs := sanitizeAndValidateURLs(urls)
	if len(cleanURLs) == 0 {
		return nil, fmt.Errorf("no valid URLs provided")
	}

	// 3. Path Security (Jailbreak Protection)
	// Mencegah user nakal melakukan path traversal (misal: ../../system)
	safeStorPath := securePath(storPath)

	// 4. Flag Optimization
	cleanFlags := optimizeFlags(flags)

	cfg := defaultTaskConfig()
	parsedCfg, cleanedFlags, err := applyControlFlags(cfg, cleanFlags)
	if err != nil {
		return nil, err
	}
	if err := validateConfig(parsedCfg); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)

	return &Task{
		ID:        id,
		CreatedAt: time.Now(),
		ctx:       ctx,
		cancel:    cancel,
		URLs:      cleanURLs,
		Flags:     cleanedFlags,
		Storage:   stor,
		StorPath:  safeStorPath,
		Meta:      make(map[string]interface{}), // Ready for future expansion
		Progress:  progressTracker,
		Config:    parsedCfg,
		Stats: TaskStats{
			StartTime: time.Now(),
			TotalURLs: len(cleanURLs),
		},
		pauseCh: make(chan struct{}),
	}, nil
}

// Title implements core.Executable.
func (t *Task) Title() string {
	urlCount := len(t.URLs)
	storageName := t.Storage.Name() // Dijamin aman karena check di NewTask
	priorityLabel := "NRM"
	if t.Config.Priority <= 0 {
		priorityLabel = "LOW"
	} else if t.Config.Priority >= 2 {
		priorityLabel = "HIGH"
	}

	// Format Log Professional: [Type] ID | Payload -> Destination
	if urlCount == 1 {
		safeURL := truncateString(t.URLs[0], 50)
		return fmt.Sprintf("[%s][%s] %s | %s ➔ %s:%s", t.Type(), priorityLabel, t.ID, safeURL, storageName, t.StorPath)
	}

	return fmt.Sprintf("[%s][%s] %s | Batch(%d URLs) ➔ %s:%s", t.Type(), priorityLabel, t.ID, urlCount, storageName, t.StorPath)
}

// Type implements core.Executable.
func (t *Task) Type() tasktype.TaskType {
	return tasktype.TaskTypeYtdlp
}

// TaskID implements core.Executable.
func (t *Task) TaskID() string {
	return t.ID
}

func (t *Task) Pause() {
	t.pauseMu.Lock()
	defer t.pauseMu.Unlock()
	if t.paused {
		return
	}
	t.paused = true
}

func (t *Task) Resume() {
	t.pauseMu.Lock()
	defer t.pauseMu.Unlock()
	if !t.paused {
		return
	}
	t.paused = false
	close(t.pauseCh)
	t.pauseCh = make(chan struct{})
}

func (t *Task) waitIfPaused(ctx context.Context) error {
	t.pauseMu.Lock()
	paused := t.paused
	pauseCh := t.pauseCh
	t.pauseMu.Unlock()

	if !paused {
		return nil
	}

	select {
	case <-pauseCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ---------------------------------------------------------
// HIGH-PERFORMANCE HELPER FUNCTIONS
// ---------------------------------------------------------

// sanitizeAndValidateURLs membersihkan spasi, deduplikasi, DAN validasi protokol.
func sanitizeAndValidateURLs(urls []string) []string {
	uniqueMap := make(map[string]bool)
	// Pre-allocate slice untuk performa memori (avoid slice resizing)
	clean := make([]string, 0, len(urls))

	for _, u := range urls {
		trimmed := strings.TrimSpace(u)
		if trimmed == "" {
			continue
		}

		// Validasi URL sesungguhnya (bukan sekadar string)
		// Kita pastikan link punya protokol (http/https) agar yt-dlp tidak bingung
		parsed, err := url.Parse(trimmed)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			// Opsional: Log invalid URL atau coba fix (tambah https://)
			// Di sini kita strict: Reject invalid scheme demi keamanan
			if !strings.HasPrefix(trimmed, "magnet:") { // Exception buat magnet link jika perlu
				continue
			}
		}

		if !uniqueMap[trimmed] {
			uniqueMap[trimmed] = true
			clean = append(clean, trimmed)
		}
	}
	return clean
}

// securePath memastikan path aman dari traversal attacks.
func securePath(path string) string {
	clean := filepath.Clean(path)

	// Security: Tolak path yang mencoba naik ke parent directory (..)
	// Jika path mengandung "..", kita paksa flat atau reject.
	// Di sini kita ambil pendekatan 'Fail Safe': ratakan ke base name jika mencurigakan.
	if strings.Contains(clean, "..") {
		// Log warning here ideally
		clean = filepath.Base(clean)
	}

	if clean == "." || clean == "/" || clean == "\\" {
		return ""
	}

	// Normalisasi slash untuk konsistensi OS (Windows/Linux)
	return filepath.ToSlash(clean)
}

// optimizeFlags membersihkan flag kosong.
func optimizeFlags(flags []string) []string {
	if len(flags) == 0 {
		return nil
	}
	clean := make([]string, 0, len(flags))
	for _, f := range flags {
		trimmed := strings.TrimSpace(f)
		if trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	return clean
}

// truncateString memotong string panjang dengan elegan.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
