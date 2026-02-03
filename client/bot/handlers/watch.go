package handlers

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"text/template"

	"github.com/celestix/gotgproto/dispatcher"
	"github.com/celestix/gotgproto/ext"
	"github.com/charmbracelet/log"
	"github.com/merisssas/Bot/client/bot/handlers/utils/mediautil"
	"github.com/merisssas/Bot/client/bot/handlers/utils/ruleutil"
	userclient "github.com/merisssas/Bot/client/user"
	"github.com/merisssas/Bot/common/i18n"
	"github.com/merisssas/Bot/common/i18n/i18nk"
	"github.com/merisssas/Bot/common/utils/tgutil"
	"github.com/merisssas/Bot/core"
	coretfile "github.com/merisssas/Bot/core/tasks/tfile"
	"github.com/merisssas/Bot/database"
	"github.com/merisssas/Bot/pkg/enums/fnamest"
	"github.com/merisssas/Bot/pkg/tfile"
	"github.com/merisssas/Bot/storage"
	"github.com/rs/xid"
)

func handleWatchCmd(ctx *ext.Context, update *ext.Update) error {
	logger := log.FromContext(ctx)
	args := strings.Split(update.EffectiveMessage.Text, " ")
	if len(args) < 2 {
		ctx.Reply(update, ext.ReplyTextString(i18n.T(i18nk.BotMsgWatchHelpText)), nil)
		return dispatcher.EndGroups
	}
	userChatID := update.GetUserChat().GetID()
	user, err := database.GetUserByChatID(ctx, userChatID)
	if err != nil {
		logger.Errorf("Failed to get user: %s", err)
		ctx.Reply(update, ext.ReplyTextString(i18n.T(i18nk.BotMsgCommonErrorGetUserFailed)), nil)
		return dispatcher.EndGroups
	}
	if user.DefaultStorage == "" {
		ctx.Reply(update, ext.ReplyTextString(i18n.T(i18nk.BotMsgCommonErrorDefaultStorageNotSet)), nil)
		return dispatcher.EndGroups
	}
	chatArg := args[1]
	chatID, err := tgutil.ParseChatID(ctx, chatArg)
	if err != nil {
		ctx.Reply(update, ext.ReplyTextString(i18n.T(i18nk.BotMsgCommonErrorInvalidIdOrUsername, map[string]any{"Error": err.Error()})), nil)
		return dispatcher.EndGroups
	}
	watching, err := user.WatchingChat(ctx, chatID)
	if err != nil {
		logger.Errorf("Failed to check if user is watching chat %d: %s", chatID, err)
		return dispatcher.EndGroups
	}
	if watching {
		ctx.Reply(update, ext.ReplyTextString(i18n.T(i18nk.BotMsgWatchInfoAlreadyWatchingChat)), nil)
		return dispatcher.EndGroups
	}
	filter := ""
	if len(args) > 2 {
		filterArg := strings.Join(args[2:], " ")
		filterType := strings.Split(filterArg, ":")[0]
		filterData := strings.Split(filterArg, ":")[1]
		if filterType == "" || filterData == "" {
			ctx.Reply(update, ext.ReplyTextString(i18n.T(i18nk.BotMsgWatchErrorFilterFormatInvalid)), nil)
			return dispatcher.EndGroups
		}
		switch filterType {
		case "msgre":
			_, err := regexp.Compile(filterData)
			if err != nil {
				ctx.Reply(update, ext.ReplyTextString(i18n.T(i18nk.BotMsgCommonErrorInvalidRegex, map[string]any{"Error": err.Error()})), nil)
				return dispatcher.EndGroups
			}
			filter = filterType + ":" + filterData
		default:
			ctx.Reply(update, ext.ReplyTextString(i18n.T(i18nk.BotMsgWatchErrorFilterTypeUnsupported)), nil)
			return dispatcher.EndGroups
		}
	}
	if err := user.WatchChat(ctx, database.WatchChat{
		UserID: user.ID,
		ChatID: chatID,
		Filter: filter,
	}); err != nil {
		logger.Errorf("Failed to watch chat %d: %s", chatID, err)
		ctx.Reply(update, ext.ReplyTextString(i18n.T(i18nk.BotMsgWatchErrorWatchChatFailed, map[string]any{"Error": err.Error()})), nil)
		return dispatcher.EndGroups
	}
	ctx.Reply(update, ext.ReplyTextString(i18n.T(i18nk.BotMsgWatchInfoWatchChatStarted, map[string]any{"Chat": chatArg})), nil)
	return dispatcher.EndGroups
}

func handleLswatchCmd(ctx *ext.Context, update *ext.Update) error {
	logger := log.FromContext(ctx)
	userChatID := update.GetUserChat().GetID()
	user, err := database.GetUserByChatID(ctx, userChatID)
	if err != nil {
		logger.Errorf("Failed to get user: %s", err)
		ctx.Reply(update, ext.ReplyTextString(i18n.T(i18nk.BotMsgCommonErrorGetUserFailed)), nil)
		return dispatcher.EndGroups
	}
	chats := user.WatchChats
	if len(chats) == 0 {
		ctx.Reply(update, ext.ReplyTextString(i18n.T(i18nk.BotMsgWatchInfoWatchListEmpty)), nil)
		return dispatcher.EndGroups
	}
	var sb strings.Builder
	sb.WriteString(i18n.T(i18nk.BotMsgWatchInfoWatchListHeader))
	for _, chat := range chats {
		sb.WriteString("- ")
		sb.WriteString(fmt.Sprintf("%d", chat.ChatID))
		if chat.Filter != "" {
			sb.WriteString(i18n.T(i18nk.BotMsgWatchInfoWatchListFilterPrefix))
			sb.WriteString(chat.Filter)
			sb.WriteString(")")
		}
		sb.WriteString("\n")
	}
	ctx.Reply(update, ext.ReplyTextString(sb.String()), nil)
	return dispatcher.EndGroups
}

func handleUnwatchCmd(ctx *ext.Context, update *ext.Update) error {
	logger := log.FromContext(ctx)
	args := strings.Split(update.EffectiveMessage.Text, " ")
	if len(args) < 2 {
		ctx.Reply(update, ext.ReplyTextString(i18n.T(i18nk.BotMsgWatchErrorUnwatchNoChatProvided)), nil)
		return dispatcher.EndGroups
	}
	userChatID := update.GetUserChat().GetID()
	user, err := database.GetUserByChatID(ctx, userChatID)
	if err != nil {
		logger.Errorf("Failed to get user: %s", err)
		ctx.Reply(update, ext.ReplyTextString(i18n.T(i18nk.BotMsgCommonErrorGetUserFailed)), nil)
		return dispatcher.EndGroups
	}
	chatArg := args[1]
	chatID, err := tgutil.ParseChatID(ctx, chatArg)
	if err != nil {
		ctx.Reply(update, ext.ReplyTextString(i18n.T(i18nk.BotMsgCommonErrorInvalidIdOrUsername, map[string]any{"Error": err.Error()})), nil)
		return dispatcher.EndGroups
	}
	if err := user.UnwatchChat(ctx, chatID); err != nil {
		logger.Errorf("Failed to unwatch chat %d: %s", chatID, err)
		ctx.Reply(update, ext.ReplyTextString(i18n.T(i18nk.BotMsgWatchErrorUnwatchChatFailed, map[string]any{"Error": err.Error()})), nil)
		return dispatcher.EndGroups
	}
	ctx.Reply(update, ext.ReplyTextString(i18n.T(i18nk.BotMsgWatchInfoWatchChatStopped, map[string]any{"Chat": chatArg})), nil)
	return dispatcher.EndGroups
}

func listenMediaMessageEvent(ch <-chan userclient.MediaBatch) {
	if userclient.GetCtx() == nil {
		return
	}
	logger := log.FromContext(userclient.GetCtx())
	for batch := range ch {
		logger.Debug("Received media batch event", "chat_id", batch.ChatID, "group_id", batch.GroupID, "event_count", len(batch.Events))
		if len(batch.Events) == 0 {
			continue
		}
		ctx := batch.Events[0].Ctx
		chats, err := database.GetWatchChatsByChatID(ctx, batch.ChatID)
		if err != nil {
			logger.Errorf("Failed to get watch chats for chat ID %d: %v", batch.ChatID, err)
			continue
		}
		for _, chat := range chats {
			albumFiles := make([]tfile.TGFileMessage, 0, len(batch.Events))
			filterEnabled := false
			filterType := ""
			filterData := ""
			if chat.Filter != "" {
				filter := strings.Split(chat.Filter, ":")
				if len(filter) != 2 {
					logger.Warnf("Invalid filter format in chat %d, skipping", chat.ChatID)
					continue
				}
				filterEnabled = true
				filterType = filter[0]
				filterData = filter[1]
			}
			user, err := database.GetUserByID(ctx, chat.UserID)
			if err != nil {
				logger.Errorf("Failed to get user by ID %d: %v", chat.UserID, err)
				continue
			}
			if user.DefaultStorage == "" {
				logger.Warnf("User %d has no default storage set, skipping media message handling", chat.UserID)
				continue
			}
			stor, err := storage.GetStorageByUserIDAndName(ctx, user.ChatID, user.DefaultStorage)
			if err != nil {
				logger.Errorf("Failed to get storage by user ID %d and name %s: %v", user.ChatID, user.DefaultStorage, err)
				continue
			}
			for _, event := range batch.Events {
				file := event.File
				msgText := file.Message().GetMessage()
				if filterEnabled {
					switch filterType {
					case "msgre": // [TODO] enums for filter types
						if ok, err := regexp.MatchString(filterData, msgText); err != nil {
							continue
						} else if !ok {
							continue
						}
					default:
						logger.Warnf("Unsupported filter type %s in chat %d, skipping", filterType, chat.ChatID)
						continue
					}
				}
				switch user.FilenameStrategy {
				case fnamest.Message.String():
					file.SetName(tgutil.GenFileNameFromMessage(*file.Message()))
				case fnamest.Template.String():
					if user.FilenameTemplate == "" {
						logger.Warnf("Empty filename template for user %d, using default filename", user.ChatID)
						break
					}
					message := file.Message()
					tmpl, err := template.New("filename").Parse(user.FilenameTemplate)
					if err != nil {
						logger.Errorf("Failed to parse filename template for user %d: %s", user.ChatID, err)
						break
					}
					data := mediautil.BuildFilenameTemplateData(message)
					var sb strings.Builder
					err = tmpl.Execute(&sb, data)
					if err != nil {
						log.FromContext(ctx).Errorf("failed to execute filename template: %s", err)
						break
					}
					file.SetName(sb.String())
				}

				needAlbumHandling := false
				if batch.GroupID != 0 && user.ApplyRule && user.Rules != nil {
					_, _, matchedDirPath := ruleutil.ApplyRule(ctx, user.Rules, ruleutil.NewInput(file))
					needAlbumHandling = matchedDirPath.NeedNewForAlbum()
				}

				if needAlbumHandling {
					albumFiles = append(albumFiles, file)
					continue
				}

				var dirPath string
				if user.ApplyRule && user.Rules != nil {
					matched, matchedStorageName, matchedDirPath := ruleutil.ApplyRule(ctx, user.Rules, ruleutil.NewInput(file))
					if !matched {
						goto startCreateTask
					}
					dirPath = matchedDirPath.String()
					if matchedStorageName.Usable() {
						stor, err = storage.GetStorageByUserIDAndName(ctx, user.ChatID, matchedStorageName.String())
						if err != nil {
							logger.Errorf("Failed to get storage by user ID and name: %s", err)
							continue
						}
					}
				}
			startCreateTask:
				storagePath := path.Join(dirPath, file.Name())
				injectCtx := tgutil.ExtWithContext(ctx.Context, ctx)
				taskid := xid.New().String()
				task, err := coretfile.NewTGFileTask(taskid, injectCtx, file, stor, storagePath, nil)
				if err != nil {
					logger.Errorf("create task failed: %s", err)
					continue
				}
				if err := core.AddTask(injectCtx, task); err != nil {
					logger.Errorf("add task failed: %s", err)
					continue
				}
				logger.Infof("Added media message task for user %d in chat %d: %s", chat.UserID, batch.ChatID, file.Name())
			}
			if len(albumFiles) > 0 {
				processWatchMediaGroup(ctx, user, stor, "", albumFiles)
			}
		}
	}
}

func processWatchMediaGroup(ctx *ext.Context, user *database.User, stor storage.Storage, dirPath string, files []tfile.TGFileMessage) {
	logger := log.FromContext(ctx)
	if len(files) == 0 {
		return
	}

	useRule := user.ApplyRule && user.Rules != nil

	applyRule := func(file tfile.TGFileMessage) (string, ruleutil.MatchedDirPath) {
		if !useRule {
			return stor.Name(), ruleutil.MatchedDirPath(dirPath)
		}
		matched, storName, dirP := ruleutil.ApplyRule(ctx, user.Rules, ruleutil.NewInput(file))
		if !matched {
			return stor.Name(), ruleutil.MatchedDirPath(dirPath)
		}
		storname := storName.String()
		if !storName.Usable() {
			storname = stor.Name()
		}
		return storname, dirP
	}

	type albumFile struct {
		file    tfile.TGFileMessage
		storage storage.Storage
	}
	albumFiles := make(map[int64][]albumFile)

	// Collect files by group ID
	for _, file := range files {
		storName, ruleDirPath := applyRule(file)
		fileStor := stor
		if storName != stor.Name() && storName != "" {
			var err error
			fileStor, err = storage.GetStorageByUserIDAndName(ctx, user.ChatID, storName)
			if err != nil {
				logger.Errorf("Failed to get storage by user ID and name: %s", err)
				continue
			}
		}

		groupId, isGroup := file.Message().GetGroupedID()
		if !isGroup || groupId == 0 {
			logger.Warnf("File %s is not in a group, skipping", file.Name())
			continue
		}

		if !ruleDirPath.NeedNewForAlbum() {
			logger.Warnf("File %s does not need album folder, skipping", file.Name())
			continue
		}

		if _, ok := albumFiles[groupId]; !ok {
			albumFiles[groupId] = make([]albumFile, 0)
		}
		albumFiles[groupId] = append(albumFiles[groupId], albumFile{
			file:    file,
			storage: fileStor,
		})
	}

	// Process album files with folder creation
	injectCtx := tgutil.ExtWithContext(ctx.Context, ctx)
	totalTasks := 0
	for groupID, afiles := range albumFiles {
		if len(afiles) <= 1 {
			continue
		}

		// Use first file's name (without extension) as album folder name
		albumDir := strings.TrimSuffix(path.Base(afiles[0].file.Name()), path.Ext(afiles[0].file.Name()))
		albumStor := afiles[0].storage

		logger.Infof("Creating album folder for group %d: %s with %d files", groupID, albumDir, len(afiles))

		for _, af := range afiles {
			afstorPath := path.Join(dirPath, albumDir, af.file.Name())
			taskid := xid.New().String()
			task, err := coretfile.NewTGFileTask(taskid, injectCtx, af.file, albumStor, afstorPath, nil)
			if err != nil {
				logger.Errorf("create task failed for album file: %s", err)
				continue
			}
			if err := core.AddTask(injectCtx, task); err != nil {
				logger.Errorf("add task failed: %s", err)
				continue
			}
			totalTasks++
		}
	}
	logger.Infof("Added %d watch media tasks for user %d", totalTasks, user.ChatID)
}
