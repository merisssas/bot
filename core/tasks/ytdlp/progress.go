package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gotd/td/telegram/message/entity"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/tg"

	"github.com/merisssas/bot/common/i18n"
	"github.com/merisssas/bot/common/i18n/i18nk"
	"github.com/merisssas/bot/common/utils/dlutil"
	"github.com/merisssas/bot/common/utils/tgutil"
)

// UI Constants for a Premium Look
const (
	updateInterval   = 3 * time.Second // Safe interval for Telegram
	progressBarWidth = 15
	charFilled       = "▓"
	charEmpty        = "░"
)

// ProgressTracker defines the interface for tracking ytdlp task progress
type ProgressTracker interface {
	OnStart(ctx context.Context, task *Task)
	OnProgress(ctx context.Context, task *Task, status string)
	OnDone(ctx context.Context, task *Task, err error)
}

func NewProgress(msgID int, userID int64) ProgressTracker {
	return &Progress{
		msgID:  msgID,
		chatID: userID,
	}
}

// Progress implements a thread-safe, rate-limited progress reporter.
type Progress struct {
	msgID  int
	chatID int64
	start  time.Time

	// State Control
	lastUpdate       time.Time
	lastStatus       string
	transferredCount atomic.Int32

	// Concurrency Control
	mu sync.Mutex // Protects non-atomic fields during render
}

// OnStart initializes the dashboard.
func (p *Progress) OnStart(ctx context.Context, task *Task) {
	logger := log.FromContext(ctx)

	p.mu.Lock()
	p.start = time.Now()
	p.lastUpdate = time.Time{} // Force first update
	p.transferredCount.Store(0)
	p.mu.Unlock()

	logger.Infof("🚀 Task Started | ID: %s | User: %d", task.ID, p.chatID)

	// Build Initial Dashboard
	p.renderDashboard(ctx, task, "🚀 Initializing...", false)
}

// OnProgress handles real-time updates with smart throttling.
func (p *Progress) OnProgress(ctx context.Context, task *Task, status string) {
	p.mu.Lock()
	now := time.Now()
	previousStatus := p.lastStatus

	// Smart Throttling: Skip update if too soon, unless status changed drastically
	// or it's been long enough.
	if now.Sub(p.lastUpdate) < updateInterval {
		// If status is identical, definitely skip
		if status == p.lastStatus {
			p.mu.Unlock()
			return
		}
		lowerStatus := strings.ToLower(status)
		if strings.Contains(lowerStatus, "%") &&
			!strings.Contains(lowerStatus, "error") &&
			!strings.Contains(lowerStatus, "finished") &&
			!strings.Contains(lowerStatus, "upload") &&
			!strings.Contains(lowerStatus, "transfer") &&
			!strings.Contains(lowerStatus, "post") {
			p.mu.Unlock()
			return
		}
	}

	p.lastUpdate = now
	p.lastStatus = status
	p.mu.Unlock()

	if hasTransferred(status) && status != previousStatus {
		p.transferredCount.Add(1)
	}

	// Render the UI
	p.renderDashboard(ctx, task, status, false)
}

// OnDone handles the final state.
func (p *Progress) OnDone(ctx context.Context, task *Task, err error) {
	logger := log.FromContext(ctx)

	if err != nil {
		if errors.Is(err, context.Canceled) {
			logger.Warnf("Task %s canceled", task.ID)
			p.renderFinalState(ctx, task, "🚫 Task Canceled by User", true)
		} else {
			logger.Errorf("Task %s failed: %v", task.ID, err)
			p.renderFinalState(ctx, task, fmt.Sprintf("❌ Error: %s", err.Error()), true)
		}
		return
	}

	logger.Infof("Task %s completed successfully", task.ID)
	p.renderFinalState(ctx, task, "✅ All Tasks Completed Successfully", false)
}

// renderDashboard constructs the "Billion Dollar" UI.
func (p *Progress) renderDashboard(ctx context.Context, task *Task, statusRaw string, isFinal bool) {
	ext := tgutil.ExtFromContext(ctx)
	if ext == nil {
		return
	}

	// Clean up status string (remove duplicate percs, etc)
	statusDisplay := cleanStatusString(statusRaw)
	elapsed := dlutil.FormatDuration(time.Since(p.start))

	// Determine Phase Icon
	phaseIcon := "⬇️"
	if strings.Contains(strings.ToLower(statusRaw), "upload") {
		phaseIcon = "☁️"
	} else if strings.Contains(strings.ToLower(statusRaw), "process") {
		phaseIcon = "⚙️"
	}

	entityBuilder := entity.Builder{}

	// --- HEADER SECTION ---
	// "📥 Task #ID (3 items)"
	headerText := i18n.T(i18nk.BotMsgProgressYtdlpStart, map[string]any{"Count": len(task.URLs)})

	if err := styling.Perform(&entityBuilder,
		styling.Bold(headerText),
		styling.Plain("\n"),
		styling.Code(fmt.Sprintf("🆔 Task ID: %s", task.TaskID())),
		styling.Plain("\n━━━━━━━━━━━━━━━━━━━━\n"),
	); err != nil {
		return
	}

	// --- BODY SECTION (Dynamic) ---
	if !isFinal {
		// Status Line with Icon
		if err := styling.Perform(&entityBuilder,
			styling.Plain(fmt.Sprintf("%s Status:\n", phaseIcon)),
			styling.Italic(statusDisplay),
			styling.Plain("\n\n"),
		); err != nil {
			return
		}

		// Stats Grid
		if err := styling.Perform(&entityBuilder,
			styling.Plain("⏱ Elapsed: "), styling.Code(elapsed),
			styling.Plain(" | 📦 Files: "), styling.Code(fmt.Sprintf("%d/%d", p.transferredCount.Load(), len(task.URLs))),
			styling.Plain("\n"),
		); err != nil {
			return
		}

		// Storage Info
		if err := styling.Perform(&entityBuilder,
			styling.Plain("📂 Save to: "),
			styling.Code(fmt.Sprintf("[%s] %s", task.storageLabel(), task.storagePathLabel())),
			styling.Plain("\n━━━━━━━━━━━━━━━━━━━━"),
		); err != nil {
			return
		}
	} else {
		// Final Summary
		if err := styling.Perform(&entityBuilder,
			styling.Bold(statusDisplay), // This contains "Success" or "Error"
			styling.Plain("\n\n"),
			styling.Plain("📊 Final Stats:\n"),
			styling.Plain("• Duration: "), styling.Code(elapsed), styling.Plain("\n"),
			styling.Plain("• Destination: "), styling.Code(fmt.Sprintf("[%s] %s", task.storageLabel(), task.storagePathLabel())),
		); err != nil {
			return
		}
	}

	text, entities := entityBuilder.Complete()

	req := &tg.MessagesEditMessageRequest{
		ID: p.msgID,
	}
	req.SetMessage(text)
	req.SetEntities(entities)

	// Don't show Cancel button if finished
	if !isFinal {
		req.SetReplyMarkup(&tg.ReplyInlineMarkup{
			Rows: []tg.KeyboardButtonRow{
				{
					Buttons: []tg.KeyboardButtonClass{
						tgutil.BuildCancelButton(task.TaskID()),
					},
				},
			},
		})
	}

	// Fire and Forget (don't block pipeline)
	// In production, you might want to handle error logging here
	tgutil.EditMessage(ext, p.chatID, req)
}

// renderFinalState is a wrapper to ensure final updates always go through
func (p *Progress) renderFinalState(ctx context.Context, task *Task, message string, isError bool) {
	// Force update ignoring throttle
	p.renderDashboard(ctx, task, message, true)
}

// Helper: Clean up raw status strings from yt-dlp/transfer
func cleanStatusString(s string) string {
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}

func hasTransferred(status string) bool {
	lowerStatus := strings.ToLower(status)
	return strings.Contains(lowerStatus, "transferred:") || strings.Contains(lowerStatus, "uploaded")
}

var _ ProgressTracker = (*Progress)(nil)
