package directlinks

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gotd/td/telegram/message/entity"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/tg"
	"github.com/merisssas/bot/common/i18n"
	"github.com/merisssas/bot/common/i18n/i18nk"
	"github.com/merisssas/bot/common/utils/dlutil"
	"github.com/merisssas/bot/common/utils/tgutil"
)

type telegramProgressCallback struct {
	msgID  int
	chatID int64
}

func NewTelegramProgress(msgID int, userID int64) ProgressTracker {
	return NewProgress(NewTelegramProgressCallback(msgID, userID))
}

func NewTelegramProgressCallback(msgID int, userID int64) ProgressCallback {
	return &telegramProgressCallback{
		msgID:  msgID,
		chatID: userID,
	}
}

func (p *telegramProgressCallback) OnStart(ctx context.Context, info TaskInfo, snapshot ProgressSnapshot) {
	p.updateMessage(ctx, info, snapshot, false)
}

func (p *telegramProgressCallback) OnProgress(ctx context.Context, info TaskInfo, snapshot ProgressSnapshot) {
	p.updateMessage(ctx, info, snapshot, false)
}

func (p *telegramProgressCallback) OnDone(ctx context.Context, info TaskInfo, snapshot ProgressSnapshot, err error) {
	if err != nil {
		p.renderError(ctx, info, err)
		return
	}
	p.updateMessage(ctx, info, snapshot, true)
}

func (p *telegramProgressCallback) updateMessage(ctx context.Context, info TaskInfo, snapshot ProgressSnapshot, isDone bool) {
	ext := tgutil.ExtFromContext(ctx)
	if ext == nil {
		return
	}

	entityBuilder := entity.Builder{}
	titleKey := i18nk.BotMsgProgressDirectUiDownloading
	titleEmoji := "⬇️ "
	if isDone {
		titleKey = i18nk.BotMsgProgressDirectUiDone
		titleEmoji = "✅ "
	}
	if err := styling.Perform(&entityBuilder,
		styling.Bold(titleEmoji+i18n.T(titleKey, nil)),
		styling.Plain("\n\n"),
	); err != nil {
		return
	}

	sizeStr := dlutil.FormatSize(snapshot.TotalBytes)
	if snapshot.TotalBytes == 0 {
		sizeStr = i18n.T(i18nk.BotMsgProgressDirectUiUnknown, nil)
	}
	if err := styling.Perform(&entityBuilder,
		styling.Plain("📦 "),
		styling.Plain(i18n.T(i18nk.BotMsgProgressDirectUiSizeLabel, nil)),
		styling.Plain(": "),
		styling.Code(sizeStr),
		styling.Plain(" | 📄 "),
		styling.Plain(i18n.T(i18nk.BotMsgProgressDirectUiFilesLabel, nil)),
		styling.Plain(": "),
		styling.Code(fmt.Sprintf("%d", snapshot.TotalFiles)),
		styling.Plain("\n"),
	); err != nil {
		return
	}

	if !isDone {
		progressBar := renderProgressBar(snapshot.Percent)
		if err := styling.Perform(&entityBuilder,
			styling.Code(fmt.Sprintf("%s %d%%", progressBar, snapshot.Percent)),
			styling.Plain("\n"),
		); err != nil {
			return
		}

		speedStr := dlutil.FormatSize(int64(snapshot.Speed)) + "/s"
		etaStr := i18n.T(i18nk.BotMsgProgressDirectUiCalculating, nil)
		if snapshot.Remaining > 0 {
			etaStr = dlutil.FormatDuration(snapshot.Remaining)
		}

		if err := styling.Perform(&entityBuilder,
			styling.Plain("🚀 "),
			styling.Plain(i18n.T(i18nk.BotMsgProgressDirectUiSpeedLabel, nil)),
			styling.Plain(": "),
			styling.Code(speedStr),
			styling.Plain(" | ⏳ "),
			styling.Plain(i18n.T(i18nk.BotMsgProgressDirectUiEtaLabel, nil)),
			styling.Plain(": "),
			styling.Code(etaStr),
			styling.Plain("\n\n"),
		); err != nil {
			return
		}

		if len(snapshot.Processing) > 0 {
			if err := styling.Perform(&entityBuilder,
				styling.Underline(i18n.T(i18nk.BotMsgProgressDirectUiProcessingTitle, nil)),
				styling.Plain("\n"),
			); err != nil {
				return
			}

			for i, file := range snapshot.Processing {
				if i >= 3 {
					break
				}
				fileName := file.FileName()
				if len(fileName) > 25 {
					fileName = fileName[:22] + "..."
				}
				if err := styling.Perform(&entityBuilder,
					styling.Plain("• "),
					styling.Code(fileName),
					styling.Plain(fmt.Sprintf(" (%s)\n", dlutil.FormatSize(file.FileSize()))),
				); err != nil {
					return
				}
			}
			if len(snapshot.Processing) > 3 {
				moreText := i18n.T(i18nk.BotMsgProgressDirectUiProcessingMore, map[string]any{
					"Count": len(snapshot.Processing) - 3,
				})
				if err := styling.Perform(&entityBuilder, styling.Italic(strings.TrimSpace(moreText))); err != nil {
					return
				}
			}
		}
	} else {
		if err := styling.Perform(&entityBuilder,
			styling.Plain("📂 "),
			styling.Plain(i18n.T(i18nk.BotMsgProgressDirectUiSavedToLabel, nil)),
			styling.Plain(" "),
			styling.Code(fmt.Sprintf("[%s] %s", info.StorageName(), info.StoragePath())),
		); err != nil {
			return
		}
	}

	text, entities := entityBuilder.Complete()
	req := &tg.MessagesEditMessageRequest{ID: p.msgID}
	req.SetMessage(text)
	req.SetEntities(entities)

	if !isDone {
		req.SetReplyMarkup(&tg.ReplyInlineMarkup{
			Rows: []tg.KeyboardButtonRow{{
				Buttons: []tg.KeyboardButtonClass{tgutil.BuildCancelButton(info.TaskID())},
			}},
		})
	}

	tgutil.EditMessage(ext, p.chatID, req)
}

func (p *telegramProgressCallback) renderError(ctx context.Context, info TaskInfo, err error) {
	ext := tgutil.ExtFromContext(ctx)
	if ext == nil {
		return
	}

	msg := i18n.T(i18nk.BotMsgProgressTaskFailedWithError, map[string]any{"Error": err.Error()})
	if errors.Is(err, context.Canceled) {
		msg = i18n.T(i18nk.BotMsgProgressTaskCanceledWithId, map[string]any{"TaskID": info.TaskID()})
	}

	tgutil.EditMessage(ext, p.chatID, &tg.MessagesEditMessageRequest{
		ID:      p.msgID,
		Message: msg,
	})
}

var _ ProgressCallback = (*telegramProgressCallback)(nil)
