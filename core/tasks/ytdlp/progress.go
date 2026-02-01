package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gotd/td/telegram/message/entity"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/tg"

	"github.com/merisssas/Bot/common/utils/tgutil"
)

// ProgressTracker defines the interface for tracking ytdlp task progress
type ProgressTracker interface {
	OnStart(ctx context.Context, task *Task)
	OnProgress(ctx context.Context, task *Task, status ProgressUpdate)
	OnDone(ctx context.Context, task *Task, err error)
}

type ProgressUpdate struct {
	Status        string
	FilePercent   float64
	TotalPercent  float64
	Speed         string
	ETA           time.Duration
	Filename      string
	ItemIndex     int
	ItemTotal     int
	FragmentIndex int
	FragmentCount int
}

type Progress struct {
	msgID  int
	chatID int64
	start  time.Time

	// State Management untuk Smart Throttling
	lastUpdate  atomic.Value // stores time.Time
	lastPercent float64      // Menyimpan persentase terakhir
	mu          sync.Mutex   // Mutex untuk menghindari race condition saat update UI
	spinnerIdx  int          // Index untuk animasi spinner
}

// Konstanta "Billion Dollar UI"
const (
	progressBarWidth = 15              // Panjang bar: [█████.....]
	minUpdateDelta   = 2.0             // Update minimal 2% perubahan baru kirim request (hemat API)
	updateInterval   = 3 * time.Second // Interval waktu paksa update
)

// Regex untuk mencuri angka persen dari string status (misal: "Downloading: 45.5%")
var percentRegex = regexp.MustCompile(`(\d+(\.\d+)?)%`)

// OnStart implements ProgressTracker.
func (p *Progress) OnStart(ctx context.Context, task *Task) {
	p.start = time.Now()
	p.lastUpdate.Store(time.Now())
	p.spinnerIdx = 0

	log.FromContext(ctx).Infof("🎬 Task Started: %s (MsgID: %d)", task.ID, p.msgID)

	p.updateMessage(ctx, task, ProgressUpdate{
		Status:       "Initializing connection...",
		FilePercent:  0,
		TotalPercent: 0,
		ItemTotal:    len(task.URLs),
	}, 0, true)
}

// OnProgress implements ProgressTracker.
func (p *Progress) OnProgress(ctx context.Context, task *Task, status ProgressUpdate) {
	// 1. Ekstrak Persentase dari string status (menggunakan Regex)
	currentPercent := status.TotalPercent
	matches := percentRegex.FindStringSubmatch(status.Status)
	if len(matches) > 1 {
		if val, err := strconv.ParseFloat(matches[1], 64); err == nil {
			currentPercent = val
		}
	}

	// 2. SMART THROTTLING LOGIC (Jantung efisiensi)
	// Kita hanya update ke Telegram jika:
	// a. Waktu sudah berlalu > 3 detik (updateInterval), ATAU
	// b. Progress naik signifikan (> 2%) sejak update terakhir, ATAU
	// c. Status mengandung kata kunci penting (misal "Completed")
	lastTime := p.lastUpdate.Load().(time.Time)
	timeSinceLast := time.Since(lastTime)

	p.mu.Lock()
	percentDelta := math.Abs(currentPercent - p.lastPercent)
	p.mu.Unlock()

	shouldUpdate := false

	// Force update jika interval tercapai
	if timeSinceLast >= updateInterval {
		shouldUpdate = true
	} else if percentDelta >= minUpdateDelta {
		// Update jika progress melompat signifikan
		shouldUpdate = true
	}

	if !shouldUpdate {
		return // Skip update untuk menghemat kuota API Telegram
	}

	// Update state
	p.lastUpdate.Store(time.Now())
	p.mu.Lock()
	p.lastPercent = currentPercent
	p.mu.Unlock()

	// 3. Render Dashboard
	p.updateMessage(ctx, task, status, currentPercent, false)
}

// updateMessage membangun UI Telegram yang cantik dan mengirimnya
func (p *Progress) updateMessage(ctx context.Context, task *Task, statusRaw ProgressUpdate, percent float64, isStart bool) {
	ext := tgutil.ExtFromContext(ctx)
	if ext == nil {
		return
	}

	// Spinner Animation: ⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏
	spinners := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	p.spinnerIdx = (p.spinnerIdx + 1) % len(spinners)
	spinner := spinners[p.spinnerIdx]

	// Header Icon
	headerIcon := "📥"
	lowerStatus := strings.ToLower(statusRaw.Status)
	if strings.Contains(lowerStatus, "upload") || strings.Contains(lowerStatus, "transfer") {
		headerIcon = "📤"
	}

	// Build Progress Bar Visual
	totalBar := renderProgressBar(statusRaw.TotalPercent, progressBarWidth)
	fileBar := renderProgressBar(statusRaw.FilePercent, progressBarWidth)

	// Bersihkan status string dari prefix yg mungkin duplikat
	displayStatus := cleanStatus(statusRaw.Status)

	entityBuilder := entity.Builder{}

	// --- CONSTRUCTION OF THE BILLION DOLLAR MESSAGE ---
	// Format:
	// 📥 Downloading 3 files... ⠋
	// [██████░░░░] 60.5%
	//
	// 📄 file_name.mp4
	// 🚀 Speed: 5MB/s | ⏳ ETA: 20s
	//
	// 💾 /storage/path/
	var err error

	// Line 1: Header
	if isStart {
		err = styling.Perform(&entityBuilder,
			styling.Plain(fmt.Sprintf("%s Starting task... %s\n", headerIcon, spinner)),
		)
	} else {
		fileLine := fmt.Sprintf("File %d/%d", statusRaw.ItemIndex, statusRaw.ItemTotal)
		if statusRaw.Filename != "" {
			fileLine = fmt.Sprintf("%s • %s", fileLine, statusRaw.Filename)
		}
		speed := statusRaw.Speed
		if speed == "" {
			speed = "-"
		}
		eta := statusRaw.ETA.Round(time.Second)
		err = styling.Perform(&entityBuilder,
			styling.Plain(fmt.Sprintf("%s Processing %d items %s\n", headerIcon, len(task.URLs), spinner)),
			styling.Code(fmt.Sprintf("TOTAL %s %.1f%%", totalBar, statusRaw.TotalPercent)),
			styling.Plain("\n"),
			styling.Code(fmt.Sprintf("FILE  %s %.1f%%", fileBar, statusRaw.FilePercent)),
			styling.Plain("\n\n"),
			styling.Plain(fmt.Sprintf("%s\n", fileLine)),
			styling.Code(displayStatus),
			styling.Plain(fmt.Sprintf("\n🚀 Speed: %s | ⏳ ETA: %s\n", speed, eta)),
		)
	}

	// Footer: Path (Always visible for verification)
	err = styling.Perform(&entityBuilder,
		styling.Plain("\n📂 Save to: "),
		styling.Code(task.Storage.Name()),
	)

	if err != nil {
		log.FromContext(ctx).Errorf("UI Build Error: %s", err)
		return
	}

	text, entities := entityBuilder.Complete()

	req := &tg.MessagesEditMessageRequest{
		ID: p.msgID,
	}
	req.SetMessage(text)
	req.SetEntities(entities)

	// Tombol Cancel selalu ada
	req.SetReplyMarkup(&tg.ReplyInlineMarkup{
		Rows: []tg.KeyboardButtonRow{
			{
				Buttons: []tg.KeyboardButtonClass{
					tgutil.BuildCancelButton(task.TaskID()),
				},
			},
		},
	})

	// Fire and Forget (Resilient Network Handling)
	// Kita tidak return error disini agar proses download utama tidak terganggu
	// hanya karena UI gagal update.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.FromContext(ctx).Error("Panic recovered in progress update")
			}
		}()
		if err := ext.EditMessage(p.chatID, req); err != nil {
			// Ignore "Message not modified" errors specifically
			if !strings.Contains(err.Error(), "MESSAGE_NOT_MODIFIED") {
				log.FromContext(ctx).Debugf("Failed to update progress UI: %v", err)
			}
		}
	}()
}

// OnDone implements ProgressTracker.
func (p *Progress) OnDone(ctx context.Context, task *Task, err error) {
	logger := log.FromContext(ctx)
	ext := tgutil.ExtFromContext(ctx)
	if ext == nil {
		return
	}

	duration := time.Since(p.start).Round(time.Second)

	if err != nil {
		// Handling Error / Cancel
		isCancel := errors.Is(err, context.Canceled)

		icon := "❌"
		title := "Task Failed"
		msg := err.Error()

		if isCancel {
			icon = "🚫"
			title = "Task Canceled"
			msg = "User requested stop."
			logger.Infof("Task %s canceled", task.ID)
		} else {
			logger.Errorf("Task %s failed: %v", task.ID, err)
		}

		ext.EditMessage(p.chatID, &tg.MessagesEditMessageRequest{
			ID:      p.msgID,
			Message: fmt.Sprintf("%s %s\n\nReason: %s\n⏱ Duration: %s", icon, title, msg, duration),
		})
		return
	}

	// SUCCESS STATE
	logger.Infof("✅ Task %s Success", task.ID)

	entityBuilder := entity.Builder{}
	styling.Perform(&entityBuilder,
		styling.Plain("✅ "),
		styling.Bold("Task Completed Successfully"),
		styling.Plain("\n\n"),
		styling.Plain(fmt.Sprintf("📦 Items: %d\n", len(task.URLs))),
		styling.Plain(fmt.Sprintf("⏱ Duration: %s\n", duration)),
		styling.Plain("📂 Location: "),
		styling.Code(fmt.Sprintf("[%s]:%s", task.Storage.Name(), task.StorPath)),
	)

	text, entities := entityBuilder.Complete()

	// Hapus tombol cancel, ganti jadi pesan final statis
	req := &tg.MessagesEditMessageRequest{
		ID: p.msgID,
	}
	req.SetMessage(text)
	req.SetEntities(entities)

	ext.EditMessage(p.chatID, req)
}

// renderProgressBar creates string: [██████░░░░]
func renderProgressBar(percent float64, width int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	// Hitung berapa blok penuh
	fullBlocks := int((percent / 100) * float64(width))

	// Bangun string
	var bar strings.Builder
	bar.WriteString("[")
	for i := 0; i < width; i++ {
		if i < fullBlocks {
			bar.WriteString("█")
		} else {
			bar.WriteString("░")
		}
	}
	bar.WriteString("]")
	return bar.String()
}

// cleanStatus removes noise from status string
func cleanStatus(s string) string {
	// Jika status terlalu panjang, potong
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}

var _ ProgressTracker = (*Progress)(nil)

func NewProgress(msgID int, userID int64) ProgressTracker {
	return &Progress{
		msgID:  msgID,
		chatID: userID,
	}
}
