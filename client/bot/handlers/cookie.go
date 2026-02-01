package handlers

import (
	"bytes"
	"errors"
	"strings"

	"github.com/celestix/gotgproto/dispatcher"
	"github.com/celestix/gotgproto/ext"
	"github.com/gotd/td/tg"

	"github.com/merisssas/bot/common/i18n"
	"github.com/merisssas/bot/common/i18n/i18nk"
	"github.com/merisssas/bot/pkg/ytdlpcookie"
)

const maxCookieSize = 1024 * 1024

func handleCookieCmd(ctx *ext.Context, u *ext.Update) error {
	userID := u.GetUserChat().GetID()
	cookieText, err := readCookieContent(ctx, u)
	if err != nil {
		switch {
		case errors.Is(err, errCookieUsage):
			ctx.Reply(u, ext.ReplyTextString(i18n.T(i18nk.BotMsgCookieUsage, nil)), nil)
		case errors.Is(err, errCookieTooLarge):
			ctx.Reply(u, ext.ReplyTextString(i18n.T(i18nk.BotMsgCookieErrorFileTooLarge, map[string]any{
				"SizeMB": maxCookieSize / 1024 / 1024,
			})), nil)
		default:
			ctx.Reply(u, ext.ReplyTextString(i18n.T(i18nk.BotMsgCookieErrorSaveFailed, map[string]any{
				"Error": err.Error(),
			})), nil)
		}
		return dispatcher.EndGroups
	}

	if _, err := ytdlpcookie.SaveForUser(userID, []byte(cookieText)); err != nil {
		ctx.Reply(u, ext.ReplyTextString(i18n.T(i18nk.BotMsgCookieErrorSaveFailed, map[string]any{
			"Error": err.Error(),
		})), nil)
		return dispatcher.EndGroups
	}

	ctx.Reply(u, ext.ReplyTextString(i18n.T(i18nk.BotMsgCookieInfoSaved, nil)), nil)
	return dispatcher.EndGroups
}

var (
	errCookieUsage    = errors.New("cookie usage")
	errCookieTooLarge = errors.New("cookie too large")
)

func readCookieContent(ctx *ext.Context, u *ext.Update) (string, error) {
	if doc, ok := findCookieDocument(u); ok {
		content, err := downloadCookieDocument(ctx, doc)
		if err != nil {
			return "", err
		}
		return content, nil
	}

	payload := extractCommandPayload(u.EffectiveMessage.Text)
	if payload != "" {
		return validateCookieText(payload)
	}

	if reply := u.EffectiveMessage.ReplyToMessage; reply != nil {
		replyText := strings.TrimSpace(reply.Text)
		if replyText != "" {
			return validateCookieText(replyText)
		}
	}

	return "", errCookieUsage
}

func findCookieDocument(u *ext.Update) (*tg.MessageMediaDocument, bool) {
	if u.EffectiveMessage.Media != nil {
		if doc, ok := u.EffectiveMessage.Media.(*tg.MessageMediaDocument); ok {
			return doc, true
		}
	}
	if u.EffectiveMessage.ReplyToMessage != nil && u.EffectiveMessage.ReplyToMessage.Media != nil {
		if doc, ok := u.EffectiveMessage.ReplyToMessage.Media.(*tg.MessageMediaDocument); ok {
			return doc, true
		}
	}
	return nil, false
}

func downloadCookieDocument(ctx *ext.Context, doc *tg.MessageMediaDocument) (string, error) {
	value, ok := doc.GetDocument()
	if !ok {
		return "", errCookieUsage
	}
	document, ok := value.AsNotEmpty()
	if !ok {
		return "", errCookieUsage
	}
	if document.Size > maxCookieSize {
		return "", errCookieTooLarge
	}
	if !strings.HasPrefix(document.MimeType, "text/") && document.MimeType != "" {
		return "", errCookieUsage
	}
	data := bytes.NewBuffer(nil)
	if _, err := ctx.DownloadMedia(doc, ext.DownloadOutputStream{Writer: data}, nil); err != nil {
		return "", err
	}
	return validateCookieText(data.String())
}

func validateCookieText(content string) (string, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", errCookieUsage
	}
	if len(trimmed) > maxCookieSize {
		return "", errCookieTooLarge
	}
	return trimmed, nil
}

func extractCommandPayload(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return ""
	}
	first := parts[0]
	if len(trimmed) == len(first) {
		return ""
	}
	return strings.TrimSpace(trimmed[len(first):])
}
