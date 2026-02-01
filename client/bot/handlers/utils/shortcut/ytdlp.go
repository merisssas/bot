package shortcut

import (
	"github.com/celestix/gotgproto/dispatcher"
	"github.com/celestix/gotgproto/ext"
	"github.com/charmbracelet/log"
	"github.com/gotd/td/tg"
	"github.com/rs/xid"

	"github.com/merisssas/bot/common/i18n"
	"github.com/merisssas/bot/common/i18n/i18nk"
	"github.com/merisssas/bot/common/utils/tgutil"
	"github.com/merisssas/bot/config"
	"github.com/merisssas/bot/core"
	"github.com/merisssas/bot/core/tasks/ytdlp"
	"github.com/merisssas/bot/pkg/ytdlpcookie"
	"github.com/merisssas/bot/storage"
)

func CreateAndAddYtdlpTaskWithEdit(ctx *ext.Context, stor storage.Storage, dirPath string, urls []string, flags []string, msgID int, userID int64, uploadToChat bool) error {
	logger := log.FromContext(ctx)
	injectCtx := tgutil.ExtWithContext(ctx.Context, ctx)

	// Validate URLs
	if len(urls) == 0 {
		logger.Error("URLs list is empty")
		tgutil.EditMessage(ctx, userID, &tg.MessagesEditMessageRequest{
			ID:      msgID,
			Message: i18n.T(i18nk.BotMsgYtdlpErrorNoValidUrls, nil),
		})
		return dispatcher.EndGroups
	}

	logger.Infof("Creating yt-dlp task for %d URL(s) with %d flag(s)", len(urls), len(flags))

	var cookiePath string
	if path, ok := ytdlpcookie.ExistsForUser(userID); ok {
		cookiePath = path
	}

	telegramPremium := false
	if cfg, ok := config.C().GetUserConfig(userID); ok {
		telegramPremium = cfg.TelegramPremium
	}

	// Create yt-dlp task
	task := ytdlp.NewTask(
		xid.New().String(),
		urls,
		flags,
		cookiePath,
		stor,
		dirPath,
		ytdlp.NewProgress(msgID, userID),
		uploadToChat,
		userID,
		telegramPremium,
	)

	// Add task to queue
	if err := core.AddTask(injectCtx, task); err != nil {
		logger.Errorf("Failed to add yt-dlp task: %s", err)
		tgutil.EditMessage(ctx, userID, &tg.MessagesEditMessageRequest{
			ID: msgID,
			Message: i18n.T(i18nk.BotMsgCommonErrorTaskAddFailed, map[string]any{
				"Error": err.Error(),
			}),
		})
		return dispatcher.EndGroups
	}

	tgutil.EditMessage(ctx, userID, &tg.MessagesEditMessageRequest{
		ID:      msgID,
		Message: i18n.T(i18nk.BotMsgCommonInfoTaskAdded, nil),
	})

	return dispatcher.EndGroups
}
