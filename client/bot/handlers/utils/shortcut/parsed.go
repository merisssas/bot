package shortcut

import (
	"github.com/celestix/gotgproto/dispatcher"
	"github.com/celestix/gotgproto/ext"
	"github.com/charmbracelet/log"
	"github.com/gotd/td/tg"
	"github.com/merisssas/bot/client/bot/handlers/utils/msgelem"
	"github.com/merisssas/bot/common/i18n"
	"github.com/merisssas/bot/common/i18n/i18nk"
	"github.com/merisssas/bot/common/utils/tgutil"
	"github.com/merisssas/bot/core"
	parsed "github.com/merisssas/bot/core/tasks/parsed"
	"github.com/merisssas/bot/pkg/parser"
	"github.com/merisssas/bot/storage"
	"github.com/rs/xid"
)

func CreateAndAddParsedTaskWithEdit(ctx *ext.Context, stor storage.Storage, dirPath string, item *parser.Item, msgID int, userID int64) error {
	injectCtx := tgutil.ExtWithContext(ctx.Context, ctx)
	task := parsed.NewTask(xid.New().String(), injectCtx, stor, dirPath, item, parsed.NewProgress(msgID, userID))
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
	text, entities := msgelem.BuildTaskAddedEntities(ctx, item.Title, core.GetLength(ctx))
	tgutil.EditMessage(ctx, userID, &tg.MessagesEditMessageRequest{
		ID:       msgID,
		Message:  text,
		Entities: entities,
	})
	return dispatcher.EndGroups
}
