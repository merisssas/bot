package shortcut

import (
	"github.com/celestix/gotgproto/dispatcher"
	"github.com/celestix/gotgproto/ext"
	"github.com/charmbracelet/log"
	"github.com/gotd/td/tg"
	"github.com/merisssas/Bot/client/bot/handlers/utils/msgelem"
	"github.com/merisssas/Bot/common/i18n"
	"github.com/merisssas/Bot/common/i18n/i18nk"
	"github.com/merisssas/Bot/common/utils/tgutil"
	"github.com/merisssas/Bot/core"
	parsed "github.com/merisssas/Bot/core/tasks/parsed"
	"github.com/merisssas/Bot/pkg/parser"
	"github.com/merisssas/Bot/storage"
	"github.com/rs/xid"
)

func CreateAndAddParsedTaskWithEdit(ctx *ext.Context, stor storage.Storage, dirPath string, item *parser.Item, msgID int, userID int64) error {
	injectCtx := tgutil.ExtWithContext(ctx.Context, ctx)
	task := parsed.NewTask(xid.New().String(), injectCtx, stor, dirPath, item, parsed.NewProgress(msgID, userID))
	if err := core.AddTask(injectCtx, task); err != nil {
		log.FromContext(ctx).Errorf("Failed to add task: %s", err)
		ctx.EditMessage(userID, &tg.MessagesEditMessageRequest{
			ID: msgID,
			Message: i18n.T(i18nk.BotMsgCommonErrorTaskAddFailed, map[string]any{
				"Error": err.Error(),
			}),
		})
		return dispatcher.EndGroups
	}
	text, entities := msgelem.BuildTaskAddedEntities(ctx, item.Title, core.GetLength(ctx))
	ctx.EditMessage(userID, &tg.MessagesEditMessageRequest{
		ID:       msgID,
		Message:  text,
		Entities: entities,
	})
	return dispatcher.EndGroups
}
