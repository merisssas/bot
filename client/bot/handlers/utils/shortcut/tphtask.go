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
	"github.com/merisssas/bot/common/utils/tphutil"
	"github.com/merisssas/bot/core"
	tphtask "github.com/merisssas/bot/core/tasks/telegraph"
	"github.com/merisssas/bot/pkg/telegraph"
	"github.com/merisssas/bot/storage"
	"github.com/rs/xid"
)

func CreateAndAddtelegraphWithEdit(
	ctx *ext.Context,
	userID int64,
	tphpage *telegraph.Page,
	dirPath string, // unescaped ph path for file storage
	pics []string,
	stor storage.Storage,
	trackMsgID int) error {

	injectCtx := tgutil.ExtWithContext(ctx.Context, ctx)
	task := tphtask.NewTask(xid.New().String(),
		injectCtx,
		tphpage.Path,
		pics,
		stor,
		dirPath,
		tphutil.DefaultClient(),
		tphtask.NewProgress(trackMsgID, userID),
	)
	if err := core.AddTask(injectCtx, task); err != nil {
		log.FromContext(ctx).Errorf("Failed to add task: %s", err)
		tgutil.EditMessage(ctx, userID, &tg.MessagesEditMessageRequest{
			ID: trackMsgID,
			Message: i18n.T(i18nk.BotMsgCommonErrorTaskAddFailed, map[string]any{
				"Error": err.Error(),
			}),
		})
		return dispatcher.EndGroups
	}
	text, entities := msgelem.BuildTaskAddedEntities(ctx, tphpage.Title, core.GetLength(ctx))
	tgutil.EditMessage(ctx, userID, &tg.MessagesEditMessageRequest{
		ID:       trackMsgID,
		Message:  text,
		Entities: entities,
	})
	return dispatcher.EndGroups
}
