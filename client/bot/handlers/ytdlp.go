package handlers

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/celestix/gotgproto/dispatcher"
	"github.com/celestix/gotgproto/ext"
	"github.com/charmbracelet/log"

	"github.com/merisssas/bot/client/bot/handlers/utils/msgelem"
	"github.com/merisssas/bot/common/i18n"
	"github.com/merisssas/bot/common/i18n/i18nk"
	"github.com/merisssas/bot/common/utils/dlutil"
	"github.com/merisssas/bot/config"
	ytdlptask "github.com/merisssas/bot/core/tasks/ytdlp"
	"github.com/merisssas/bot/pkg/enums/tasktype"
	"github.com/merisssas/bot/pkg/tcbdata"
	"github.com/merisssas/bot/pkg/ytdlpcookie"
	"github.com/merisssas/bot/storage"
)

func handleYtdlpCmd(ctx *ext.Context, update *ext.Update) error {
	logger := log.FromContext(ctx)
	userID := update.GetUserChat().GetID()
	args := strings.Split(update.EffectiveMessage.Text, " ")
	if len(args) < 2 {
		ctx.Reply(update, ext.ReplyTextString(i18n.T(i18nk.BotMsgYtdlpUsage)), nil)
		return dispatcher.EndGroups
	}

	// Separate URLs and flags from arguments
	var urls []string
	var flags []string

	for i := 1; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}

		// Check if it's a flag (starts with - or --)
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			// Check if the next argument might be a value for this flag
			// Don't consume it if it starts with - or looks like a URL with scheme
			if i+1 < len(args) {
				nextArg := strings.TrimSpace(args[i+1])
				if nextArg != "" && !strings.HasPrefix(nextArg, "-") {
					// Check if it's clearly a URL (has ://)
					// This handles common video URLs (http://, https://)
					// For other yt-dlp inputs, users should ensure proper formatting
					if strings.Contains(nextArg, "://") {
						// It's a URL, don't consume it as a flag value
						continue
					}
					// Otherwise, treat it as a flag value
					flags = append(flags, nextArg)
					i++ // Skip the next argument as it's been consumed
				}
			}
		} else {
			// Try to parse as URL
			u, err := url.Parse(arg)
			if err != nil || u.Scheme == "" || u.Host == "" {
				logger.Warnf("Invalid URL: %s", arg)
				continue
			}
			urls = append(urls, arg)
		}
	}

	if len(urls) == 0 {
		ctx.Reply(update, ext.ReplyTextString(i18n.T(i18nk.BotMsgYtdlpErrorNoValidUrls)), nil)
		return dispatcher.EndGroups
	}

	flags = applyYtdlpUserSettings(userID, flags)

	logger.Debugf("Preparing yt-dlp download for %d URL(s) with %d flag(s)", len(urls), len(flags))

	if len(urls) == 1 && !hasFormatFlag(flags) {
		var cookiePath string
		if path, ok := ytdlpcookie.ExistsForUser(userID); ok {
			cookiePath = path
		}
		choices, err := ytdlptask.FetchFormatChoices(ctx, urls[0], cookiePath)
		if err != nil {
			logger.Warnf("Failed to fetch yt-dlp formats: %v", err)
		} else {
			qualityOptions := make([]tcbdata.YtdlpQualityChoice, 0, len(choices))
			for _, choice := range choices {
				label := formatChoiceLabel(choice, choice.EstimatedSize)
				qualityOptions = append(qualityOptions, tcbdata.YtdlpQualityChoice{
					Label:         label,
					URLs:          urls,
					Flags:         append(flags, choice.Flags...),
					EstimatedSize: choice.EstimatedSize,
				})
			}
			markup, err := msgelem.BuildYtdlpQualityKeyboard(qualityOptions)
			if err != nil {
				logger.Warnf("Failed to build yt-dlp quality keyboard: %v", err)
			} else {
				ctx.Reply(update, ext.ReplyTextString(i18n.T(i18nk.BotMsgYtdlpSelectQuality, nil)), &ext.ReplyOpts{
					Markup: markup,
				})
				return dispatcher.EndGroups
			}
		}
	}

	// Build storage selection keyboard
	markup, err := msgelem.BuildAddSelectStorageKeyboard(storage.GetUserStorages(ctx, userID), tcbdata.Add{
		TaskType:   tasktype.TaskTypeYtdlp,
		YtdlpURLs:  urls,
		YtdlpFlags: flags,
	})
	if err != nil {
		return err
	}

	ctx.Reply(update, ext.ReplyTextString(i18n.T(i18nk.BotMsgYtdlpInfoUrlsSelectStorage, map[string]any{
		"Count": len(urls),
	})), &ext.ReplyOpts{
		Markup: markup,
	})

	return dispatcher.EndGroups
}

func hasFormatFlag(flags []string) bool {
	for _, flag := range flags {
		switch flag {
		case "-f", "--format":
			return true
		}
	}
	return false
}

func applyYtdlpUserSettings(userID int64, flags []string) []string {
	if hasSponsorBlockFlag(flags) {
		return flags
	}
	cfg, ok := config.C().GetUserConfig(userID)
	if !ok || !cfg.YtdlpSponsorBlock {
		return flags
	}
	return append(flags, "--sponsorblock-remove", "all")
}

func hasSponsorBlockFlag(flags []string) bool {
	for i := 0; i < len(flags); i++ {
		if flags[i] == "--sponsorblock-remove" {
			return true
		}
	}
	return false
}

func formatChoiceLabel(choice ytdlptask.FormatChoice, size int64) string {
	label := choiceLabel(choice)
	sizeLabel := ""
	if size > 0 {
		sizeLabel = dlutil.FormatSize(size)
	}
	return formatChoiceLabelWithSize(label, sizeLabel)
}

func choiceLabel(choice ytdlptask.FormatChoice) string {
	if choice.IsAudioOnly {
		return i18n.T(i18nk.BotMsgYtdlpQualityAudioOnlyBest, nil)
	}
	extras := ""
	if len(choice.Extras) > 0 {
		extras = " " + strings.Join(choice.Extras, " ")
	}
	return i18n.T(i18nk.BotMsgYtdlpQualityVideo, map[string]any{
		"Resolution": fmt.Sprintf("%dp", choice.Height),
		"Extras":     extras,
		"Ext":        strings.ToUpper(choice.Extension),
	})
}

func formatChoiceLabelWithSize(label, size string) string {
	if size == "" {
		return label
	}
	return fmt.Sprintf("%s | %s", label, size)
}
