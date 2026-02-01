package shortcut

import (
	"fmt"
	"strings"

	"github.com/celestix/gotgproto/dispatcher"
	"github.com/celestix/gotgproto/ext"
	"github.com/charmbracelet/log"
	"github.com/gotd/td/tg"
	"github.com/merisssas/Bot/common/i18n"
	"github.com/merisssas/Bot/common/i18n/i18nk"
	"github.com/merisssas/Bot/common/utils/tgutil"
	"github.com/merisssas/Bot/core"
	"github.com/merisssas/Bot/core/tasks/aria2dl"
	"github.com/merisssas/Bot/pkg/aria2"
	"github.com/merisssas/Bot/storage"
	"github.com/rs/xid"
)

func CreateAndAddAria2TaskWithEdit(ctx *ext.Context, stor storage.Storage, dirPath string, uris []string, aria2Client *aria2.Client, msgID int, userID int64) error {
	logger := log.FromContext(ctx)
	injectCtx := tgutil.ExtWithContext(ctx.Context, ctx)

	// Now add to aria2 after user selected storage
	logger.Infof("Adding download to aria2, uris type: %T, value: %+v", uris, uris)

	// Ensure uris is valid
	cleanedURIs, err := aria2dl.ValidateURIs(uris)
	if err != nil {
		logger.Errorf("URIs validation failed: %v", err)
		ctx.EditMessage(userID, &tg.MessagesEditMessageRequest{
			ID:      msgID,
			Message: i18n.T(i18nk.BotMsgDlErrorNoValidLinks, nil),
		})
		return dispatcher.EndGroups
	}

	taskConfig := aria2dl.DefaultTaskConfig()
	options, err := aria2dl.BuildAria2Options(taskConfig)
	if err != nil {
		logger.Errorf("Failed to build aria2 options: %v", err)
		ctx.EditMessage(userID, &tg.MessagesEditMessageRequest{
			ID: msgID,
			Message: i18n.T(i18nk.BotMsgAria2ErrorAddingAria2Download, map[string]any{
				"Error": err.Error(),
			}),
		})
		return dispatcher.EndGroups
	}

	if taskConfig.DryRun {
		result, err := aria2dl.DryRun(ctx, taskConfig, cleanedURIs)
		if err != nil {
			logger.Errorf("Dry-run failed: %v", err)
			ctx.EditMessage(userID, &tg.MessagesEditMessageRequest{
				ID: msgID,
				Message: i18n.T(i18nk.BotMsgAria2ErrorAddingAria2Download, map[string]any{
					"Error": err.Error(),
				}),
			})
			return dispatcher.EndGroups
		}

		dryRunMessage := formatDryRunSummary(result)
		ctx.EditMessage(userID, &tg.MessagesEditMessageRequest{
			ID:      msgID,
			Message: dryRunMessage,
		})
		return dispatcher.EndGroups
	}

	gid, err := aria2Client.AddURI(ctx, cleanedURIs, options)
	if err != nil {
		logger.Errorf("Failed to add aria2 download: %s", err)
		ctx.EditMessage(userID, &tg.MessagesEditMessageRequest{
			ID: msgID,
			Message: i18n.T(i18nk.BotMsgAria2ErrorAddingAria2Download, map[string]any{
				"Error": err.Error(),
			}),
		})
		return dispatcher.EndGroups
	}
	logger.Infof("Aria2 download added with GID: %s", gid)

	if err := aria2dl.ApplyQueuePriority(ctx, aria2Client, gid, taskConfig.Priority); err != nil {
		logger.Warnf("Failed to apply aria2 queue priority: %v", err)
	}

	// Create task with the GID
	task := aria2dl.NewTask(xid.New().String(), injectCtx, gid, cleanedURIs, aria2Client, stor, dirPath, aria2dl.NewProgress(msgID, userID), aria2dl.WithTaskConfig(taskConfig))
	if err := core.AddTask(injectCtx, task); err != nil {
		logger.Errorf("Failed to add task: %s", err)
		ctx.EditMessage(userID, &tg.MessagesEditMessageRequest{
			ID: msgID,
			Message: i18n.T(i18nk.BotMsgCommonErrorTaskAddFailed, map[string]any{
				"Error": err.Error(),
			}),
		})
		return dispatcher.EndGroups
	}
	ctx.EditMessage(userID, &tg.MessagesEditMessageRequest{
		ID:      msgID,
		Message: i18n.T(i18nk.BotMsgCommonInfoTaskAdded, nil),
	})
	return dispatcher.EndGroups
}

func formatDryRunSummary(result *aria2dl.DryRunResult) string {
	if result == nil {
		return i18n.T(i18nk.BotMsgAria2DryRunEmpty, nil)
	}

	var builder strings.Builder
	builder.WriteString(i18n.T(i18nk.BotMsgAria2DryRunHeader, nil))

	if len(result.Files) == 0 {
		builder.WriteString("\n")
		builder.WriteString(i18n.T(i18nk.BotMsgAria2DryRunEmpty, nil))
		return builder.String()
	}

	for _, file := range result.Files {
		sizeLabel := i18n.T(i18nk.BotMsgAria2DryRunUnknownSize, nil)
		if file.Length > 0 {
			sizeLabel = aria2dl.FormatBytes(file.Length)
		}
		contentType := file.ContentType
		if strings.TrimSpace(contentType) == "" {
			contentType = i18n.T(i18nk.BotMsgAria2DryRunUnknownType, nil)
		}
		builder.WriteString("\n")
		builder.WriteString(fmt.Sprintf("• %s (%s, %s)", file.FileName, sizeLabel, contentType))
	}

	if len(result.Skipped) > 0 {
		builder.WriteString("\n\n")
		builder.WriteString(i18n.T(i18nk.BotMsgAria2DryRunSkipped, map[string]any{
			"Count": len(result.Skipped),
		}))
	}

	return builder.String()
}
