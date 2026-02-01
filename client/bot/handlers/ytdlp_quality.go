package handlers

import (
	"strings"

	"github.com/celestix/gotgproto/dispatcher"
	"github.com/celestix/gotgproto/ext"
	"github.com/charmbracelet/log"
	"github.com/gotd/td/tg"

	"github.com/merisssas/bot/client/bot/handlers/utils/msgelem"
	"github.com/merisssas/bot/client/bot/handlers/utils/shortcut"
	"github.com/merisssas/bot/common/i18n"
	"github.com/merisssas/bot/common/i18n/i18nk"
	"github.com/merisssas/bot/common/utils/tgutil"
	"github.com/merisssas/bot/config"
	ytdlptask "github.com/merisssas/bot/core/tasks/ytdlp"
	"github.com/merisssas/bot/pkg/enums/tasktype"
	"github.com/merisssas/bot/pkg/tcbdata"
	"github.com/merisssas/bot/storage"
)

func handleYtdlpQualityCallback(ctx *ext.Context, update *ext.Update) error {
	dataid := strings.Split(string(update.CallbackQuery.Data), " ")[1]
	data, err := shortcut.GetCallbackDataWithAnswer[tcbdata.YtdlpQualityChoice](ctx, update, dataid)
	if err != nil {
		return err
	}
	queryID := update.CallbackQuery.GetQueryID()
	msgID := update.CallbackQuery.GetMsgID()
	userID := update.CallbackQuery.GetUserID()

	stors := storage.GetUserStorages(ctx, userID)
	markup, err := msgelem.BuildAddSelectStorageKeyboard(stors, tcbdata.Add{
		TaskType:   tasktype.TaskTypeYtdlp,
		YtdlpURLs:  data.URLs,
		YtdlpFlags: data.Flags,
	})
	if err != nil {
		log.FromContext(ctx).Errorf("Failed to build storage keyboard: %s", err)
		ctx.AnswerCallback(msgelem.AlertCallbackAnswer(queryID, i18n.T(i18nk.BotMsgCommonErrorBuildStorageSelectKeyboardFailed, map[string]any{
			"Error": err.Error(),
		})))
		return dispatcher.EndGroups
	}

	if shouldOfferTelegramUpload(userID, data.EstimatedSize) {
		markup, err = msgelem.AppendYtdlpUploadButton(markup, i18n.T(i18nk.BotMsgYtdlpUploadNative, nil), tcbdata.YtdlpSend{
			URLs:          data.URLs,
			Flags:         data.Flags,
			EstimatedSize: data.EstimatedSize,
		})
		if err != nil {
			log.FromContext(ctx).Errorf("Failed to append telegram upload button: %s", err)
		}
	}

	tgutil.EditMessage(ctx, userID, &tg.MessagesEditMessageRequest{
		ID:          msgID,
		Message:     i18n.T(i18nk.BotMsgYtdlpSelectDestination, nil),
		ReplyMarkup: markup,
	})

	return dispatcher.EndGroups
}

func handleYtdlpSendCallback(ctx *ext.Context, update *ext.Update) error {
	dataid := strings.Split(string(update.CallbackQuery.Data), " ")[1]
	data, err := shortcut.GetCallbackDataWithAnswer[tcbdata.YtdlpSend](ctx, update, dataid)
	if err != nil {
		return err
	}
	msgID := update.CallbackQuery.GetMsgID()
	userID := update.CallbackQuery.GetUserID()

	return shortcut.CreateAndAddYtdlpTaskWithEdit(ctx, nil, "", data.URLs, data.Flags, msgID, userID, true)
}

func shouldOfferTelegramUpload(userID int64, size int64) bool {
	if size <= 0 {
		return false
	}
	cfg, ok := config.C().GetUserConfig(userID)
	if !ok {
		return size <= ytdlptask.MaxTelegramUploadSize(false)
	}
	return size <= ytdlptask.MaxTelegramUploadSize(cfg.TelegramPremium)
}
