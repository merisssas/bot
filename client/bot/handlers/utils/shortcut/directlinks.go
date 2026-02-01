package shortcut

import (
	"github.com/celestix/gotgproto/dispatcher"
	"github.com/celestix/gotgproto/ext"
	"github.com/charmbracelet/log"
	"github.com/gotd/td/tg"
	"github.com/merisssas/bot/common/i18n"
	"github.com/merisssas/bot/common/i18n/i18nk"
	"github.com/merisssas/bot/common/utils/tgutil"
	"github.com/merisssas/bot/core"
	"github.com/merisssas/bot/core/tasks/directlinks"
	"github.com/merisssas/bot/storage"
	"github.com/rs/xid"
)

func CreateAndAddDirectTaskWithEdit(ctx *ext.Context, stor storage.Storage, dirPath string, links []string, msgID int, userID int64) error {
	injectCtx := tgutil.ExtWithContext(ctx.Context, ctx)
	task := directlinks.NewTask(xid.New().String(), injectCtx, links, stor, dirPath, directlinks.NewTelegramProgress(msgID, userID))
	if err := core.AddTask(injectCtx, task); err != nil {
		log.FromContext(ctx).Errorf("Failed to add task: %s", err)
		tgutil.EditMessage(ctx, userID, &tg.MessagesEditMessageRequest{
			ID: msgID,
			Message: i18n.T(i18nk.BotMsgCommonErrorTaskAddFailed, map[string]any{
				"Error": err.Error(),
			}),
		})
		return dispatcher.EndGroups
	}
	tgutil.EditMessage(ctx, userID, &tg.MessagesEditMessageRequest{
		Message: i18n.T(i18nk.BotMsgCommonInfoTaskAdded, nil),
	})
	return dispatcher.EndGroups
}
