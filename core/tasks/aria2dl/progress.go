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

	"github.com/celestix/gotgproto/ext"
	"github.com/charmbracelet/log"
	"github.com/duke-git/lancet/v2/retry"
	"github.com/gotd/td/telegram/message/entity"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/merisssas/bot/common/i18n"
	"github.com/merisssas/bot/common/i18n/i18nk"
	"github.com/merisssas/bot/common/utils/dlutil"
	"github.com/merisssas/bot/common/utils/tgutil"
	"github.com/merisssas/bot/config"
	"github.com/merisssas/bot/pkg/aria2"
)

// UI Constants for a premium look.
const (
	progressBarWidth = 15
	blockFull        = "▓"
	blockEmpty       = "░"
	minFloodWait     = 5 * time.Second
)

type ProgressTracker interface {
	OnStart(ctx context.Context, task *Task)
	OnProgress(ctx context.Context, task *Task, status *aria2.Status)
	OnDone(ctx context.Context, task *Task, err error)
}

type Progress struct {
	msgID  int
	chatID int64
	start  time.Time

	lastUpdate  time.Time
	lastPercent int
	lastStatus  string

	updateInterval time.Duration
	floodWaitUntil time.Time

	mu sync.Mutex
}

// OnStart implements ProgressTracker.
func (p *Progress) OnStart(ctx context.Context, task *Task) {
	logger := log.FromContext(ctx)
	p.mu.Lock()
	p.start = time.Now()
	p.lastPercent = 0
	p.lastUpdate = time.Now()
	p.updateInterval = progressUpdateInterval()
	p.mu.Unlock()
	logger.Infof("Aria2 task started: message_id=%d, chat_id=%d, gid=%s", p.msgID, p.chatID, task.GID)
	p.updateUI(ctx, task, nil, "start", nil)
}

// OnProgress implements ProgressTracker.
func (p *Progress) OnProgress(ctx context.Context, task *Task, status *aria2.Status) {
	if status == nil {
		return
	}

	totalLength, _ := strconv.ParseInt(status.TotalLength, 10, 64)
	completedLength, _ := strconv.ParseInt(status.CompletedLength, 10, 64)
	if totalLength == 0 {
		totalLength = 1
	}
	if completedLength > totalLength {
		completedLength = totalLength
	}

	percent := int((completedLength * 100) / totalLength)
	p.mu.Lock()
	if time.Now().Before(p.floodWaitUntil) {
		p.mu.Unlock()
		return
	}

	timeElapsed := time.Since(p.lastUpdate) >= p.adaptiveInterval(percent)
	significantChange := math.Abs(float64(percent-p.lastPercent)) >= 2
	statusChanged := status.Status != p.lastStatus

	if !timeElapsed && !significantChange && !statusChanged {
		p.mu.Unlock()
		return
	}

	p.lastPercent = percent
	p.lastStatus = status.Status
	p.lastUpdate = time.Now()
	p.mu.Unlock()

	log.FromContext(ctx).Debugf("Aria2 progress update: %s, %d/%d", task.GID, completedLength, totalLength)

	p.updateUI(ctx, task, status, "progress", nil)
}

// OnDone implements ProgressTracker.
func (p *Progress) OnDone(ctx context.Context, task *Task, err error) {
	if err != nil {
		if errors.Is(err, context.Canceled) {
			p.updateUI(ctx, task, nil, "cancel", nil)
		} else {
			p.updateUI(ctx, task, nil, "error", err)
		}
		return
	}
	p.updateUI(ctx, task, nil, "done", nil)
}

var _ ProgressTracker = (*Progress)(nil)

func NewProgress(msgID int, userID int64) ProgressTracker {
	return &Progress{
		msgID:          msgID,
		chatID:         userID,
		updateInterval: progressUpdateInterval(),
	}
}

func renderProgressBar(percent int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := int((float64(percent) / 100.0) * float64(progressBarWidth))
	empty := progressBarWidth - filled
	return strings.Repeat(blockFull, filled) + strings.Repeat(blockEmpty, empty)
}

func progressUpdateInterval() time.Duration {
	intervalSeconds := config.C().Aria2.ProgressUpdateIntervalSeconds
	if intervalSeconds <= 0 {
		return 3 * time.Second
	}
	return time.Duration(intervalSeconds) * time.Second
}

func (p *Progress) adaptiveInterval(percent int) time.Duration {
	interval := p.updateInterval
	if percent <= 10 || percent >= 95 {
		interval = interval / 2
	}
	if interval < time.Second {
		interval = time.Second
	}
	return interval
}

func (p *Progress) updateUI(ctx context.Context, task *Task, status *aria2.Status, phase string, errVal error) {
	ext := tgutil.ExtFromContext(ctx)
	if ext == nil {
		return
	}

	entityBuilder := entity.Builder{}

	switch phase {
	case "start":
		if err := styling.Perform(&entityBuilder,
			styling.Bold("🚀 "+i18n.T(i18nk.BotMsgProgressAria2UiInitializing, nil)),
			styling.Plain("\n"),
			styling.Code(fmt.Sprintf("ID: %s", task.GID)),
		); err != nil {
			log.FromContext(ctx).Errorf("Failed to build entities: %s", err)
			return
		}
	case "progress":
		if status == nil {
			return
		}
		total, _ := strconv.ParseInt(status.TotalLength, 10, 64)
		completed, _ := strconv.ParseInt(status.CompletedLength, 10, 64)
		speed, _ := strconv.ParseInt(status.DownloadSpeed, 10, 64)
		uploadSpeed, _ := strconv.ParseInt(status.UploadSpeed, 10, 64)

		percent := 0
		if total > 0 {
			percent = int((completed * 100) / total)
		}

		statusIcon, statusText := p.statusLabel(status, completed, total, uploadSpeed)

		etaLabel := i18n.T(i18nk.BotMsgProgressAria2UiCalculating, nil)
		if speed > 0 && total > completed {
			remainingBytes := total - completed
			etaDuration := time.Duration(remainingBytes/speed) * time.Second
			etaLabel = dlutil.FormatDuration(etaDuration)
		}

		if err := styling.Perform(&entityBuilder,
			styling.Bold(fmt.Sprintf("%s %s", statusIcon, statusText)),
			styling.Plain("\n\n"),
			styling.Plain("📦 "),
			styling.Plain(i18n.T(i18nk.BotMsgProgressAria2UiSizeLabel, nil)),
			styling.Plain(": "),
			styling.Code(fmt.Sprintf("%s / %s", dlutil.FormatSize(completed), dlutil.FormatSize(total))),
			styling.Plain("\n"),
			styling.Plain("🚀 "),
			styling.Plain(i18n.T(i18nk.BotMsgProgressAria2UiSpeedLabel, nil)),
			styling.Plain(": "),
			styling.Bold(fmt.Sprintf("%s/s", dlutil.FormatSize(speed))),
			styling.Plain(" | ⏳ "),
			styling.Plain(i18n.T(i18nk.BotMsgProgressAria2UiEtaLabel, nil)),
			styling.Plain(": "),
			styling.Plain(etaLabel),
			styling.Plain("\n"),
		); err != nil {
			log.FromContext(ctx).Errorf("Failed to build entities: %s", err)
			return
		}

		if uploadSpeed > 0 {
			if err := styling.Perform(&entityBuilder,
				styling.Plain("📤 "),
				styling.Plain(i18n.T(i18nk.BotMsgProgressAria2UiUploadLabel, nil)),
				styling.Plain(": "),
				styling.Code(fmt.Sprintf("%s/s", dlutil.FormatSize(uploadSpeed))),
				styling.Plain("\n"),
			); err != nil {
				log.FromContext(ctx).Errorf("Failed to build entities: %s", err)
				return
			}
		}

		bar := renderProgressBar(percent)
		if err := styling.Perform(&entityBuilder,
			styling.Plain("\n"),
			styling.Code(fmt.Sprintf("%s %d%%", bar, percent)),
			styling.Plain("\n"),
		); err != nil {
			log.FromContext(ctx).Errorf("Failed to build entities: %s", err)
			return
		}

		conns, _ := strconv.Atoi(status.Connections)
		if err := styling.Perform(&entityBuilder,
			styling.Italic(fmt.Sprintf("📡 %s: %d", i18n.T(i18nk.BotMsgProgressAria2UiConnectionsLabel, nil), conns)),
		); err != nil {
			log.FromContext(ctx).Errorf("Failed to build entities: %s", err)
			return
		}
	case "done":
		if err := styling.Perform(&entityBuilder,
			styling.Bold("✅ "+i18n.T(i18nk.BotMsgProgressAria2UiDone, nil)),
			styling.Plain("\n\n"),
			styling.Plain("📂 "),
			styling.Plain(i18n.T(i18nk.BotMsgProgressAria2UiSavedToLabel, nil)),
			styling.Plain(": "),
			styling.Code(fmt.Sprintf("[%s] %s", task.Storage.Name(), task.StorPath)),
		); err != nil {
			log.FromContext(ctx).Errorf("Failed to build entities: %s", err)
			return
		}
	case "error":
		if errVal == nil {
			return
		}
		if err := styling.Perform(&entityBuilder,
			styling.Bold("❌ "+i18n.T(i18nk.BotMsgProgressAria2UiFailed, nil)),
			styling.Plain("\n\n"),
			styling.Code(errVal.Error()),
		); err != nil {
			log.FromContext(ctx).Errorf("Failed to build entities: %s", err)
			return
		}
	case "cancel":
		if err := styling.Perform(&entityBuilder,
			styling.Bold("🚫 "+i18n.T(i18nk.BotMsgProgressTaskCanceled, nil)),
			styling.Plain("\n"),
			styling.Code(fmt.Sprintf("ID: %s", task.TaskID())),
		); err != nil {
			log.FromContext(ctx).Errorf("Failed to build entities: %s", err)
			return
		}
	}

	text, entities := entityBuilder.Complete()
	req := &tg.MessagesEditMessageRequest{
		ID: p.msgID,
	}
	req.SetMessage(text)
	req.SetEntities(entities)

	if phase == "progress" || phase == "start" {
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

	p.sendWithBackoff(ctx, ext, req)
}

func (p *Progress) statusLabel(status *aria2.Status, completed, total, uploadSpeed int64) (string, string) {
	switch {
	case status.Status == "paused":
		return "⏸️", i18n.T(i18nk.BotMsgProgressAria2UiStatusPaused, nil)
	case status.VerifyIntegrityPending == "true":
		return "🔄", i18n.T(i18nk.BotMsgProgressAria2UiStatusVerifying, nil)
	case completed == total && total > 0 && uploadSpeed > 0:
		return "📤", i18n.T(i18nk.BotMsgProgressAria2UiStatusSeeding, nil)
	default:
		return "⬇️", i18n.T(i18nk.BotMsgProgressAria2UiStatusDownloading, nil)
	}
}

func (p *Progress) sendWithBackoff(ctx context.Context, extCtx *ext.Context, req *tg.MessagesEditMessageRequest) {
	retryTimes := config.C().Retry
	if retryTimes < 1 {
		retryTimes = 1
	}

	err := retry.Retry(func() error {
		_, err := tgutil.EditMessage(extCtx, p.chatID, req)
		return err
	},
		retry.Context(ctx),
		retry.RetryTimes(uint(retryTimes)),
		retry.RetryWithExponentialWithJitterBackoff(500*time.Millisecond, 2, 200*time.Millisecond),
	)

	if err == nil {
		return
	}

	if rpcErr, ok := tgerr.AsType(err, "FLOOD_WAIT"); ok {
		waitSec := rpcErr.Argument
		if waitSec < 1 {
			waitSec = 1
		}
		waitDuration := time.Duration(waitSec) * time.Second
		if waitDuration < minFloodWait {
			waitDuration = minFloodWait
		}

		p.mu.Lock()
		p.floodWaitUntil = time.Now().Add(waitDuration)
		p.updateInterval += 2 * time.Second
		p.mu.Unlock()

		log.FromContext(ctx).Warnf("Telegram FloodWait: pausing UI updates for %s", waitDuration)
		return
	}

	log.FromContext(ctx).Debugf("UI update skipped: %v", err)
}
