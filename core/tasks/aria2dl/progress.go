package aria2dl

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/celestix/gotgproto/ext"
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
	renderInterval   = 3 * time.Second
	progressBarWidth = 15
)

type ProgressTracker interface {
	OnStart(ctx context.Context, task *Task)
	OnProgress(ctx context.Context, task *Task, status *aria2.Status)
	OnDone(ctx context.Context, task *Task, err error)
}

type Progress struct {
	msgID     int
	chatID    int64
	startTime time.Time

	latestStatus atomic.Value

	stopCh  chan struct{}
	doneCh  chan struct{}
	once    sync.Once
	started atomic.Bool

	sender *ext.Context
}

func NewProgress(msgID int, userID int64) ProgressTracker {
	return &Progress{
		msgID:   msgID,
		chatID:  userID,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
		started: atomic.Bool{},
	}
}

// OnStart implements ProgressTracker.
func (p *Progress) OnStart(ctx context.Context, task *Task) {
	ext := tgutil.ExtFromContext(ctx)
	if ext == nil {
		log.FromContext(ctx).Error("UI initialized without valid Telegram extension")
		close(p.doneCh)
		return
	}

	p.startTime = time.Now()
	p.sender = ext
	p.started.Store(true)

	logger := log.FromContext(ctx)
	logger.Infof("UI started: Task %s (GID: %s)", task.TaskID(), task.GID())

	p.updateMessage(context.Background(), task, nil, i18n.T(i18nk.BotMsgProgressAria2UiInitializing, nil), false)

	go p.renderLoop(task)
}

// OnProgress implements ProgressTracker.
func (p *Progress) OnProgress(_ context.Context, _ *Task, status *aria2.Status) {
	if status == nil {
		return
	}

	p.latestStatus.Store(status)
}

// OnDone implements ProgressTracker.
func (p *Progress) OnDone(ctx context.Context, task *Task, err error) {
	logger := log.FromContext(ctx)
	p.once.Do(func() {
		close(p.stopCh)
	})
	if p.started.Load() {
		<-p.doneCh
	}

	duration := time.Since(p.startTime).Round(time.Second)

	if err != nil {
		if errors.Is(err, context.Canceled) {
			logger.Info("UI: Task canceled")
			p.editRaw(i18n.T(i18nk.BotMsgProgressAria2UiCanceled, map[string]any{
				"TaskID": task.TaskID(),
			}))
		} else {
			logger.Errorf("UI: Task failed: %v", err)
			p.editRaw(i18n.T(i18nk.BotMsgProgressAria2UiFailed, map[string]any{
				"Error": err.Error(),
			}))
		}
		return
	}

	logger.Info("UI: Task completed successfully")
	p.sendSuccessMessage(task, duration)
}

func (p *Progress) renderLoop(task *Task) {
	defer close(p.doneCh)

	ticker := time.NewTicker(renderInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			value := p.latestStatus.Load()
			if value == nil {
				continue
			}
			status := value.(*aria2.Status)
			p.updateMessage(context.Background(), task, status, "", false)
		}
	}
}

func (p *Progress) updateMessage(_ context.Context, task *Task, status *aria2.Status, customStatus string, isDone bool) {
	if p.sender == nil {
		return
	}

	state := calculateState(status)
	header := customStatus
	if header == "" {
		if status != nil {
			header = i18n.T(i18nk.BotMsgProgressAria2UiDownloadingHeader, map[string]any{
				"GID": task.GID(),
			})
		} else {
			header = i18n.T(i18nk.BotMsgProgressAria2UiPreparing, nil)
		}
	}

	entityBuilder := entity.Builder{}
	_ = styling.Perform(&entityBuilder, styling.Bold(header), styling.Plain("\n"))

	if state.Peers != "" {
		_ = styling.Perform(&entityBuilder, styling.Plain(state.Peers), styling.Plain("\n"))
	}

	_ = styling.Perform(&entityBuilder,
		styling.Code(fmt.Sprintf("%s %.1f%%", state.Bar, state.Percent)), styling.Plain("\n\n"),
		styling.Plain("💾 "), styling.Bold(fmt.Sprintf("%s / %s", state.CompletedStr, state.TotalStr)), styling.Plain("\n"),
		styling.Plain("⚡ "), styling.Code(state.SpeedStr),
		styling.Plain("  |  "),
		styling.Plain("⏳ "), styling.Code(state.EtaStr),
	)

	text, entities := entityBuilder.Complete()
	req := &tg.MessagesEditMessageRequest{
		ID:       p.msgID,
		Message:  text,
		Entities: entities,
	}

	if !isDone {
		req.ReplyMarkup = &tg.ReplyInlineMarkup{
			Rows: []tg.KeyboardButtonRow{
				{
					Buttons: []tg.KeyboardButtonClass{tgutil.BuildCancelButton(task.TaskID())},
				},
			},
		}
	}

	p.sender.EditMessage(p.chatID, req)
}

func (p *Progress) sendSuccessMessage(task *Task, duration time.Duration) {
	if p.sender == nil {
		return
	}

	entityBuilder := entity.Builder{}
	_ = styling.Perform(&entityBuilder,
		styling.Plain("✅ "), styling.Bold(i18n.T(i18nk.BotMsgProgressAria2UiCompletedTitle, nil)), styling.Plain("\n\n"),
		styling.Plain("📄 "), styling.Code(task.Title()), styling.Plain("\n"),
		styling.Plain("💾 "), styling.Code(task.Storage.Name()), styling.Plain("\n"),
		styling.Plain("⏱️ "), styling.Code(i18n.T(i18nk.BotMsgProgressAria2UiDuration, map[string]any{
			"Duration": duration.String(),
		})), styling.Plain("\n"),
		styling.Plain("📂 "), styling.Code(task.StorPath),
	)

	text, entities := entityBuilder.Complete()
	p.sender.EditMessage(p.chatID, &tg.MessagesEditMessageRequest{
		ID:       p.msgID,
		Message:  text,
		Entities: entities,
		ReplyMarkup: &tg.ReplyInlineMarkup{
			Rows: []tg.KeyboardButtonRow{},
		},
	})
}

func (p *Progress) editRaw(rawHTML string) {
	if p.sender == nil {
		return
	}
	p.sender.EditMessage(p.chatID, &tg.MessagesEditMessageRequest{
		ID:      p.msgID,
		Message: rawHTML,
	})
}

type uiState struct {
	Percent      float64
	TotalStr     string
	CompletedStr string
	SpeedStr     string
	EtaStr       string
	Peers        string
	Bar          string
}

func calculateState(status *aria2.Status) uiState {
	if status == nil {
		return uiState{
			TotalStr:     i18n.T(i18nk.BotMsgProgressAria2UiUnknownTotal, nil),
			CompletedStr: i18n.T(i18nk.BotMsgProgressAria2UiZeroCompleted, nil),
			SpeedStr:     i18n.T(i18nk.BotMsgProgressAria2UiZeroSpeed, nil),
			EtaStr:       i18n.T(i18nk.BotMsgProgressAria2UiEtaUnknown, nil),
			Bar:          drawProgressBar(0),
		}
	}

	total, completed := getAria2Totals(status)
	speed, _ := strconv.ParseFloat(status.DownloadSpeed, 64)

	var percent float64
	if total > 0 {
		percent = (completed / total) * 100
	}

	state := uiState{
		Percent:      percent,
		TotalStr:     humanizeBytes(total),
		CompletedStr: humanizeBytes(completed),
		SpeedStr:     humanizeBytes(speed) + "/s",
		Bar:          drawProgressBar(percent),
		Peers:        formatTorrentPeers(status),
	}

	if completed == 0 {
		state.CompletedStr = i18n.T(i18nk.BotMsgProgressAria2UiZeroCompleted, nil)
	}

	if total == 0 {
		state.TotalStr = i18n.T(i18nk.BotMsgProgressAria2UiUnknownTotal, nil)
	}

	if speed == 0 {
		state.SpeedStr = i18n.T(i18nk.BotMsgProgressAria2UiZeroSpeed, nil)
	}

	if speed > 0 && total > completed {
		seconds := (total - completed) / speed
		if seconds < 86400 {
			state.EtaStr = time.Duration(seconds * float64(time.Second)).Round(time.Second).String()
		} else {
			state.EtaStr = i18n.T(i18nk.BotMsgProgressAria2UiEtaOverDay, nil)
		}
	} else {
		state.EtaStr = i18n.T(i18nk.BotMsgProgressAria2UiEtaUnknown, nil)
	}

	return state
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

func drawProgressBar(percent float64) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	fullChars := int((percent / 100) * float64(progressBarWidth))
	if fullChars > progressBarWidth {
		fullChars = progressBarWidth
	}

	return fmt.Sprintf("[%s%s]", strings.Repeat("■", fullChars), strings.Repeat("□", progressBarWidth-fullChars))
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

func formatTorrentPeers(status *aria2.Status) string {
	if status == nil {
		return ""
	}

	seeders, _ := strconv.Atoi(status.NumSeeders)
	connections, _ := strconv.Atoi(status.Connections)
	if seeders == 0 && connections == 0 {
		return ""
	}

	leechers := connections - seeders
	if leechers < 0 {
		leechers = 0
	}

	return i18n.T(i18nk.BotMsgProgressAria2UiPeers, map[string]any{
		"Seeders":  seeders,
		"Leechers": leechers,
	})
}

var _ ProgressTracker = (*Progress)(nil)
