package shortcut

import (
	"github.com/celestix/gotgproto/dispatcher"
	"github.com/celestix/gotgproto/ext"
	"github.com/charmbracelet/log"
	"github.com/gotd/td/tg"
	"github.com/merisssas/bot/common/i18n"
	"github.com/merisssas/bot/common/i18n/i18nk"
	"github.com/merisssas/bot/common/utils/tgutil"
	"github.com/merisssas/bot/config"
	"github.com/merisssas/bot/core"
	"github.com/merisssas/bot/core/tasks/aria2dl"
	"github.com/merisssas/bot/pkg/aria2"
	"github.com/merisssas/bot/storage"
	"github.com/rs/xid"
)

func CreateAndAddAria2TaskWithEdit(ctx *ext.Context, stor storage.Storage, dirPath string, uris []string, aria2Client *aria2.Client, msgID int, userID int64) error {
	logger := log.FromContext(ctx)
	injectCtx := tgutil.ExtWithContext(ctx.Context, ctx)

	// Now add to aria2 after user selected storage
	logger.Infof("Adding download to aria2, uris type: %T, value: %+v", uris, uris)

	// Ensure uris is valid
	if len(uris) == 0 {
		logger.Error("URIs list is empty")
		tgutil.EditMessage(ctx, userID, &tg.MessagesEditMessageRequest{
			ID:      msgID,
			Message: i18n.T(i18nk.BotMsgDlErrorNoValidLinks, nil),
		})
		return dispatcher.EndGroups
	}

	options := buildAria2Options()
	if len(options) == 0 {
		options = nil
	}
	gid, err := aria2Client.AddURI(ctx, uris, options)
	if err != nil {
		logger.Errorf("Failed to add aria2 download: %s", err)
		tgutil.EditMessage(ctx, userID, &tg.MessagesEditMessageRequest{
			ID: msgID,
			Message: i18n.T(i18nk.BotMsgAria2ErrorAddingAria2Download, map[string]any{
				"Error": err.Error(),
			}),
		})
		return dispatcher.EndGroups
	}
	logger.Infof("Aria2 download added with GID: %s", gid)

	// Create task with the GID
	task := aria2dl.NewTask(xid.New().String(), gid, uris, aria2Client, stor, dirPath, aria2dl.NewProgress(msgID, userID))
	if err := core.AddTask(injectCtx, task); err != nil {
		logger.Errorf("Failed to add task: %s", err)
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

func buildAria2Options() aria2.Options {
	cfg := config.C().Aria2
	options := aria2.Options{}

	if cfg.MaxConnectionPerServer > 0 {
		options["max-connection-per-server"] = cfg.MaxConnectionPerServer
	}
	if cfg.Split > 0 {
		options["split"] = cfg.Split
	}
	if cfg.MinSplitSize != "" {
		options["min-split-size"] = cfg.MinSplitSize
	}

	if cfg.Continue {
		options["continue"] = "true"
	} else {
		options["continue"] = "false"
	}

	if cfg.MaxTries > 0 {
		options["max-tries"] = cfg.MaxTries
	} else if config.C().Retry > 0 {
		options["max-tries"] = config.C().Retry
	}
	if cfg.RetryWaitSeconds > 0 {
		options["retry-wait"] = cfg.RetryWaitSeconds
	}

	return options
}
