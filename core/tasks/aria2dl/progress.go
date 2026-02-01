package aria2dl

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gotd/td/telegram/message/entity"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/tg"
	"github.com/merisssas/Bot/common/i18n"
	"github.com/merisssas/Bot/common/i18n/i18nk"
	"github.com/merisssas/Bot/common/utils/tgutil"
	"github.com/merisssas/Bot/pkg/aria2"
)

const (
	progressWidth  = 15
	updateInterval = 3 * time.Second
)

type ProgressTracker interface {
	OnStart(ctx context.Context, task *Task)
	OnProgress(ctx context.Context, task *Task, status *aria2.Status)
	OnDone(ctx context.Context, task *Task, err error)
}

type Progress struct {
	msgID      int
	chatID     int64
	startTime  time.Time
	lastUpdate time.Time
	mu         sync.Mutex
}

func NewProgress(msgID int, userID int64) ProgressTracker {
	return &Progress{
		msgID:  msgID,
		chatID: userID,
	}
}

// OnStart implements ProgressTracker.
func (p *Progress) OnStart(ctx context.Context, task *Task) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.startTime = time.Now()
	p.lastUpdate = time.Now()

	logger := log.FromContext(ctx)
	logger.Infof("UI started: Task %s (GID: %s)", task.TaskID(), task.GID())

	p.updateMessage(ctx, task, nil, i18n.T(i18nk.BotMsgProgressAria2UiInitializing, nil), false)
}

// OnProgress implements ProgressTracker.
func (p *Progress) OnProgress(ctx context.Context, task *Task, status *aria2.Status) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.lastUpdate.IsZero() && time.Since(p.lastUpdate) < updateInterval {
		return
	}

	p.lastUpdate = time.Now()
	p.updateMessage(ctx, task, status, "", false)
}

// OnDone implements ProgressTracker.
func (p *Progress) OnDone(ctx context.Context, task *Task, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	logger := log.FromContext(ctx)
	ext := tgutil.ExtFromContext(ctx)
	if ext == nil {
		return
	}

	duration := time.Since(p.startTime).Round(time.Second)

	if err != nil {
		if errors.Is(err, context.Canceled) {
			logger.Info("Task canceled")
			p.editRaw(ctx, i18n.T(i18nk.BotMsgProgressAria2UiCanceled, map[string]any{
				"TaskID": task.TaskID(),
			}))
		} else {
			logger.Errorf("Task failed: %v", err)
			p.editRaw(ctx, i18n.T(i18nk.BotMsgProgressAria2UiFailed, map[string]any{
				"Error": err.Error(),
			}))
		}
		return
	}

	logger.Info("Task completed successfully")

	entityBuilder := entity.Builder{}
	if err := styling.Perform(&entityBuilder,
		styling.Plain("✅ "), styling.Bold(i18n.T(i18nk.BotMsgProgressAria2UiCompletedTitle, nil)), styling.Plain("\n\n"),
		styling.Plain("📄 "), styling.Code(task.Title()), styling.Plain("\n"),
		styling.Plain("💾 "), styling.Code(task.Storage.Name()), styling.Plain("\n"),
		styling.Plain("⏱️ "), styling.Code(i18n.T(i18nk.BotMsgProgressAria2UiDuration, map[string]any{
			"Duration": duration.String(),
		})), styling.Plain("\n"),
		styling.Plain("📂 "), styling.Code(task.StorPath),
	); err != nil {
		logger.Errorf("Failed to build entities: %v", err)
		return
	}

	text, entities := entityBuilder.Complete()
	req := &tg.MessagesEditMessageRequest{
		ID:       p.msgID,
		Message:  text,
		Entities: entities,
		ReplyMarkup: &tg.ReplyInlineMarkup{
			Rows: []tg.KeyboardButtonRow{},
		},
	}
	ext.EditMessage(p.chatID, req)
}

func (p *Progress) updateMessage(ctx context.Context, task *Task, status *aria2.Status, customStatus string, isDone bool) {
	ext := tgutil.ExtFromContext(ctx)
	if ext == nil {
		return
	}

	var (
		percent      float64
		totalStr     string
		completedStr string
		speedStr     string
		etaStr       string
		bar          string
		header       string
	)

	if status != nil {
		total, completed := getAria2Totals(status)
		speed, _ := strconv.ParseFloat(status.DownloadSpeed, 64)

		if total > 0 {
			percent = (completed / total) * 100
			totalStr = humanizeBytes(total)
			bar = drawProgressBar(percent, progressWidth)
		} else {
			totalStr = i18n.T(i18nk.BotMsgProgressAria2UiUnknownTotal, nil)
			bar = drawProgressBar(0, progressWidth)
		}

		if completed > 0 {
			completedStr = humanizeBytes(completed)
		} else {
			completedStr = i18n.T(i18nk.BotMsgProgressAria2UiZeroCompleted, nil)
		}

		if speed > 0 {
			speedStr = humanizeBytes(speed) + "/s"
		} else {
			speedStr = i18n.T(i18nk.BotMsgProgressAria2UiZeroSpeed, nil)
		}

		if speed > 0 && total > completed {
			remainingSeconds := (total - completed) / speed
			if remainingSeconds < 86400 {
				etaDuration := time.Duration(remainingSeconds) * time.Second
				etaStr = etaDuration.Round(time.Second).String()
			} else {
				etaStr = i18n.T(i18nk.BotMsgProgressAria2UiEtaOverDay, nil)
			}
		} else {
			etaStr = i18n.T(i18nk.BotMsgProgressAria2UiEtaUnknown, nil)
		}

		header = i18n.T(i18nk.BotMsgProgressAria2UiDownloadingHeader, map[string]any{
			"GID": task.GID(),
		})
	} else {
		header = i18n.T(i18nk.BotMsgProgressAria2UiPreparing, nil)
		bar = drawProgressBar(0, progressWidth)
		etaStr = i18n.T(i18nk.BotMsgProgressAria2UiCalculating, nil)
		speedStr = i18n.T(i18nk.BotMsgProgressAria2UiZeroSpeed, nil)
		totalStr = i18n.T(i18nk.BotMsgProgressAria2UiUnknownTotal, nil)
		completedStr = i18n.T(i18nk.BotMsgProgressAria2UiZeroCompleted, nil)
	}

	if customStatus != "" {
		header = customStatus
	}

	entityBuilder := entity.Builder{}
	if err := styling.Perform(&entityBuilder,
		styling.Bold(header), styling.Plain("\n"),
		styling.Code(fmt.Sprintf("%s %.1f%%", bar, percent)), styling.Plain("\n\n"),
		styling.Plain("📦 "), styling.Bold(fmt.Sprintf("%s / %s", completedStr, totalStr)), styling.Plain("\n"),
		styling.Plain("🚀 "), styling.Code(speedStr),
		styling.Plain("  |  "),
		styling.Plain("⏳ "), styling.Code(etaStr),
	); err != nil {
		log.FromContext(ctx).Error("Failed to build UI entities")
		return
	}

	text, entities := entityBuilder.Complete()
	req := &tg.MessagesEditMessageRequest{
		ID:       p.msgID,
		Message:  text,
		Entities: entities,
	}

	if !isDone {
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

	ext.EditMessage(p.chatID, req)
}

func (p *Progress) editRaw(ctx context.Context, rawHTML string) {
	ext := tgutil.ExtFromContext(ctx)
	if ext == nil {
		return
	}
	ext.EditMessage(p.chatID, &tg.MessagesEditMessageRequest{
		ID:      p.msgID,
		Message: rawHTML,
	})
}

func getAria2Totals(status *aria2.Status) (float64, float64) {
	total, _ := strconv.ParseFloat(status.TotalLength, 64)
	completed, _ := strconv.ParseFloat(status.CompletedLength, 64)

	if total > 0 && completed > 0 {
		return total, completed
	}

	var (
		filesTotal     float64
		filesCompleted float64
	)

	for _, file := range status.Files {
		fileLength, _ := strconv.ParseFloat(file.Length, 64)
		fileCompleted, _ := strconv.ParseFloat(file.CompletedLength, 64)
		filesTotal += fileLength
		filesCompleted += fileCompleted
	}

	if total == 0 && filesTotal > 0 {
		total = filesTotal
	}

	if completed == 0 && filesCompleted > 0 {
		completed = filesCompleted
	}

	return total, completed
}

func drawProgressBar(percent float64, width int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	fullChars := int((percent / 100) * float64(width))
	emptyChars := width - fullChars

	if fullChars < 0 {
		fullChars = 0
	}
	if emptyChars < 0 {
		emptyChars = 0
	}

	return fmt.Sprintf("[%s%s]", strings.Repeat("█", fullChars), strings.Repeat("░", emptyChars))
}

func humanizeBytes(value float64) string {
	sizes := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	if value < 10 {
		return fmt.Sprintf("%.0f B", value)
	}
	exp := math.Floor(math.Log(value) / math.Log(1024))
	if exp < 0 {
		exp = 0
	}
	if int(exp) >= len(sizes) {
		exp = float64(len(sizes) - 1)
	}
	suffix := sizes[int(exp)]
	val := value / math.Pow(1024, exp)
	return fmt.Sprintf("%.2f %s", val, suffix)
}

var _ ProgressTracker = (*Progress)(nil)
