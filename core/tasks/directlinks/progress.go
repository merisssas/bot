package directlinks

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/celestix/gotgproto/ext"
	"github.com/charmbracelet/log"
	"github.com/duke-git/lancet/v2/slice"
	"github.com/gotd/td/telegram/message/entity"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/tg"
	"github.com/merisssas/Bot/common/i18n"
	"github.com/merisssas/Bot/common/i18n/i18nk"
	"github.com/merisssas/Bot/common/utils/dlutil"
	"github.com/merisssas/Bot/common/utils/tgutil"
)

// TaskInfo defines the interface for task status
// It is used by the progress tracker to render current state.
type TaskInfo interface {
	TotalBytes() int64
	TotalFiles() int
	TaskID() string
	StorageName() string
	StoragePath() string
	DownloadedBytes() int64
	Processing() []FileInfo
}

// FileInfo defines the interface for file details
// shown in the progress message.
type FileInfo interface {
	FileName() string
	FileSize() int64
	DownloadedBytes() int64
}

// ProgressTracker defines the callbacks
// for updating progress state.
type ProgressTracker interface {
	OnStart(ctx context.Context, info TaskInfo)
	OnProgress(ctx context.Context, info TaskInfo)
	OnDone(ctx context.Context, info TaskInfo, err error)
}

// Progress struct with thread-safe state management
type Progress struct {
	msgID  int
	chatID int64
	start  time.Time

	// Mutex protects the state to prevent race conditions during high-concurrency downloads
	mu                sync.Mutex
	lastUpdatePercent int
	lastUpdateTime    time.Time
}

// NewProgress creates a new tracker
func NewProgress(msgID int, userID int64) ProgressTracker {
	return &Progress{
		msgID:  msgID,
		chatID: userID,
	}
}

// OnStart initializes the task display
func (p *Progress) OnStart(ctx context.Context, info TaskInfo) {
	p.mu.Lock()
	p.start = time.Now()
	p.lastUpdatePercent = 0
	p.lastUpdateTime = time.Now()
	p.mu.Unlock()

	logger := log.FromContext(ctx)
	logger.Infof("Task started: %s", info.TaskID())

	ext := tgutil.ExtFromContext(ctx)
	if ext == nil {
		return
	}

	p.sendUpdate(ctx, info, "Initializing...", nil)
}

// OnProgress handles real-time updates with throttling and visuals
func (p *Progress) OnProgress(ctx context.Context, info TaskInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()

	shouldUpdate, newPercent := shouldUpdateProgress(
		info.TotalBytes(),
		info.DownloadedBytes(),
		p.lastUpdatePercent,
		p.lastUpdateTime,
	)

	if !shouldUpdate {
		return
	}

	p.lastUpdatePercent = newPercent
	p.lastUpdateTime = time.Now()

	p.sendUpdate(ctx, info, "", nil)
}

// OnDone handles completion or failure
func (p *Progress) OnDone(ctx context.Context, info TaskInfo, err error) {
	logger := log.FromContext(ctx)
	ext := tgutil.ExtFromContext(ctx)

	if err != nil {
		p.handleError(ctx, info, err, logger, ext)
		return
	}

	logger.Infof("Task completed: %s", info.TaskID())

	duration := time.Since(p.start).Round(time.Second)
	var avgSpeed float64
	if duration.Seconds() > 0 {
		avgSpeed = float64(info.TotalBytes()) / duration.Seconds()
	}

	entityBuilder := entity.Builder{}
	if err := styling.Perform(&entityBuilder,
		styling.Bold(i18n.T(i18nk.BotMsgProgressDirectDonePrefix, nil)),
		styling.Plain("\n"),
		styling.Plain("📦 "), styling.Code(fmt.Sprintf("%d Files", info.TotalFiles())),
		styling.Plain(" | 💾 "), styling.Code(FormatBytes(info.TotalBytes())),
		styling.Plain("\n"),
		styling.Plain("⏱️ "), styling.Code(duration.String()),
		styling.Plain(" | 🚀 "), styling.Code(FormatBytes(int64(avgSpeed))+"/s"),
		styling.Plain("\n\n"),
		styling.Plain(i18n.T(i18nk.BotMsgProgressSavePathPrefix, nil)),
		styling.Code(fmt.Sprintf("[%s]: %s", info.StorageName(), info.StoragePath())),
	); err != nil {
		logger.Errorf("Failed to build entities: %s", err)
		return
	}

	text, entities := entityBuilder.Complete()
	if ext != nil {
		ext.EditMessage(p.chatID, &tg.MessagesEditMessageRequest{
			ID:       p.msgID,
			Message:  text,
			Entities: entities,
		})
	}
}

// ==========================================
// INTERNAL HELPERS & UI LOGIC
// ==========================================

func (p *Progress) sendUpdate(ctx context.Context, info TaskInfo, customStatus string, err error) {
	ext := tgutil.ExtFromContext(ctx)
	if ext == nil {
		return
	}

	total := info.TotalBytes()
	downloaded := info.DownloadedBytes()

	var percentage float64
	if total > 0 {
		percentage = float64(downloaded) / float64(total) * 100
	}

	speed := dlutil.GetSpeed(downloaded, p.start)

	eta := "∞"
	if speed > 0 && total > downloaded {
		remainingBytes := total - downloaded
		secondsLeft := float64(remainingBytes) / speed
		duration := time.Duration(secondsLeft) * time.Second
		eta = duration.Round(time.Second).String()
	}

	statusHeader := i18n.T(i18nk.BotMsgProgressDownloadingPrefix, nil)
	if customStatus != "" {
		statusHeader = customStatus
	}

	entityBuilder := entity.Builder{}
	stylingErr := styling.Perform(&entityBuilder,
		styling.Bold(statusHeader),
		styling.Plain("\n"),
		styling.Code(generateProgressBar(percentage)),
		styling.Plain(" "),
		styling.Bold(fmt.Sprintf("%.2f%%", percentage)),
		styling.Plain("\n"),
		styling.Plain("💾 "),
		styling.Code(fmt.Sprintf("%s / %s", FormatBytes(downloaded), FormatBytes(total))),
		styling.Plain("\n"),
		styling.Plain("🚀 "),
		styling.Code(FormatBytes(int64(speed))+"/s"),
		styling.Plain(" | ⏳ "),
		styling.Code(eta),
		styling.Plain("\n\n"),
		styling.Plain(i18n.T(i18nk.BotMsgProgressProcessingListPrefix, nil)),
		styling.Plain("\n"),
		func() styling.StyledTextOption {
			var lines []string
			processing := info.Processing()
			limit := 5

			for i, elem := range processing {
				if i >= limit {
					lines = append(lines, fmt.Sprintf("...and %d more", len(processing)-limit))
					break
				}
				progress := "?"
				if elem.FileSize() > 0 {
					percentage := float64(elem.DownloadedBytes()) / float64(elem.FileSize()) * 100
					progress = fmt.Sprintf("%.1f%%", percentage)
				}
				lines = append(lines, fmt.Sprintf("• %s (%s/%s, %s)", elem.FileName(), FormatBytes(elem.DownloadedBytes()), FormatBytes(elem.FileSize()), progress))
			}
			if len(lines) == 0 {
				lines = append(lines, i18n.T(i18nk.BotMsgProgressProcessingNone, nil))
			}
			return styling.Code(slice.Join(lines, "\n"))
		}(),
	)

	if stylingErr != nil {
		log.FromContext(ctx).Errorf("Failed to build entities: %s", stylingErr)
		return
	}

	text, entities := entityBuilder.Complete()

	req := &tg.MessagesEditMessageRequest{
		ID:       p.msgID,
		Message:  text,
		Entities: entities,
	}

	req.SetReplyMarkup(&tg.ReplyInlineMarkup{
		Rows: []tg.KeyboardButtonRow{{
			Buttons: []tg.KeyboardButtonClass{
				tgutil.BuildCancelButton(info.TaskID()),
			},
		}},
	})

	ext.EditMessage(p.chatID, req)
}

func (p *Progress) handleError(ctx context.Context, info TaskInfo, err error, logger *log.Logger, ext *ext.Context) {
	var msg string

	if errors.Is(err, context.Canceled) {
		logger.Infof("Task canceled: %s", info.TaskID())
		msg = i18n.T(i18nk.BotMsgProgressTaskCanceledWithId, map[string]any{
			"TaskID": info.TaskID(),
		})
	} else {
		logger.Errorf("Task failed: %s, err: %v", info.TaskID(), err)
		msg = i18n.T(i18nk.BotMsgProgressTaskFailedWithError, map[string]any{
			"Error": err.Error(),
		})
	}

	if ext != nil {
		ext.EditMessage(p.chatID, &tg.MessagesEditMessageRequest{
			ID:      p.msgID,
			Message: msg,
		})
	}
}

// generateProgressBar creates a visual string like [████░░░░░░]
func generateProgressBar(percentage float64) string {
	const barLength = 10
	if percentage < 0 {
		percentage = 0
	}
	if percentage > 100 {
		percentage = 100
	}

	filledLength := int(math.Round((percentage / 100) * float64(barLength)))

	bar := ""
	for i := 0; i < barLength; i++ {
		if i < filledLength {
			bar += "█"
		} else {
			bar += "░"
		}
	}
	return fmt.Sprintf("[%s]", bar)
}

var _ ProgressTracker = (*Progress)(nil)
