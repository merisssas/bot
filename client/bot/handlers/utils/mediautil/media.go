package mediautil

import (
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/celestix/gotgproto/ext"
	"github.com/charmbracelet/log"
	"github.com/gotd/td/tg"
	"github.com/merisssas/Bot/common/utils/strutil"
	"github.com/merisssas/Bot/common/utils/tgutil"
	"github.com/merisssas/Bot/database"
	"github.com/merisssas/Bot/pkg/enums/fnamest"
	"github.com/merisssas/Bot/pkg/tfile"
)

func IsSupported(media tg.MessageMediaClass) bool {
	switch media.(type) {
	case *tg.MessageMediaDocument, *tg.MessageMediaPhoto:
		return true
	default:
		return false
	}
}

type FilenameTemplateData struct {
	MsgID    string `json:"msgid,omitempty"`
	MsgTags  string `json:"msgtags,omitempty"`
	MsgGen   string `json:"msggen,omitempty"`
	MsgDate  string `json:"msgdate,omitempty"`
	MsgRaw   string `json:"msgraw,omitempty"`
	OrigName string `json:"origname,omitempty"`
	ChatID   string `json:"chatid,omitempty"`
}

func (f FilenameTemplateData) ToMap() map[string]string {
	return map[string]string{
		"msgid":    f.MsgID,
		"msgtags":  f.MsgTags,
		"msggen":   f.MsgGen,
		"msgraw":   f.MsgRaw,
		"msgdate":  f.MsgDate,
		"origname": f.OrigName,
		"chatid":   f.ChatID,
	}
}

func TfileOptions(ctx *ext.Context, user *database.User, message *tg.Message) []tfile.TGFileOption {
	opts := make([]tfile.TGFileOption, 0)
	var fnameOpt tfile.TGFileOption
	switch user.FilenameStrategy {
	case fnamest.Message.String():
		fnameOpt = tfile.WithName(tgutil.GenFileNameFromMessage(*message))
	case fnamest.Template.String():
		if user.FilenameTemplate == "" {
			log.FromContext(ctx).Warnf("empty filename template")
			fnameOpt = tfile.WithNameIfEmpty(tgutil.GenFileNameFromMessage(*message))
			break
		}
		tmpl, err := template.New("filename").Parse(user.FilenameTemplate)
		if err != nil {
			log.FromContext(ctx).Errorf("failed to parse filename template: %s", err)
			fnameOpt = tfile.WithNameIfEmpty(tgutil.GenFileNameFromMessage(*message))
			break
		}
		data := BuildFilenameTemplateData(message)
		var sb strings.Builder
		err = tmpl.Execute(&sb, data)
		if err != nil {
			log.FromContext(ctx).Errorf("failed to execute filename template: %s", err)
			fnameOpt = tfile.WithNameIfEmpty(tgutil.GenFileNameFromMessage(*message))
			break
		}
		fnameOpt = tfile.WithName(sb.String())
	default:
		fnameOpt = tfile.WithNameIfEmpty(tgutil.GenFileNameFromMessage(*message))
	}
	refresher := tfile.WithRefresher(func(refreshCtx context.Context) (tg.InputFileLocationClass, int64, string, error) {
		if refreshCtx != nil {
			if err := refreshCtx.Err(); err != nil {
				return nil, 0, "", err
			}
		}
		chatID := tgutil.ChatIdFromPeer(message.GetPeerID())
		if chatID == 0 {
			return nil, 0, "", fmt.Errorf("missing chat ID for message %d", message.GetID())
		}
		freshMsg, err := tgutil.GetMessageByID(ctx, chatID, message.GetID())
		if err != nil {
			return nil, 0, "", err
		}
		media, ok := freshMsg.GetMedia()
		if !ok || media == nil {
			return nil, 0, "", fmt.Errorf("message %d has no media", freshMsg.GetID())
		}
		freshFile, err := tfile.FromMediaMessage(media, ctx.Raw, freshMsg, tfile.WithNameIfEmpty(tgutil.GenFileNameFromMessage(*freshMsg)))
		if err != nil {
			return nil, 0, "", err
		}
		return freshFile.Location(), freshFile.Size(), freshFile.Name(), nil
	})
	opts = append(opts, fnameOpt, tfile.WithMessage(message), refresher)
	return opts
}

func BuildFilenameTemplateData(message *tg.Message) map[string]string {
	data := FilenameTemplateData{
		MsgID: func() string {
			id := message.GetID()
			if id == 0 {
				return ""
			}
			return fmt.Sprintf("%d", id)
		}(),
		MsgTags: func() string {
			tags := strutil.ExtractTagsFromText(message.GetMessage())
			if len(tags) == 0 {
				return ""
			}
			return strings.Join(tags, "_")
		}(),
		MsgGen: tgutil.GenFileNameFromMessage(*message),
		OrigName: func() string {
			f, _ := tgutil.GetMediaFileName(message.Media)
			return f
		}(),
		MsgDate: func() string {
			date := message.GetDate()
			if date == 0 {
				return ""
			}
			t := time.Unix(int64(date), 0)
			return t.Format("2006-01-02_15-04-05")
		}(),
		MsgRaw: message.GetMessage(),
		ChatID: func() string {
			// If the message is from a channel (fetched via a link), use its chat ID,
			// regardless of whether it was forwarded.
			if message.GetPost() {
				peer := message.GetPeerID()
				switch p := peer.(type) {
				case *tg.PeerChannel:
					return intToStringOmitZero(p.ChannelID)
				default: // impossible case
					return intToStringOmitZero(tgutil.ChatIdFromPeer(peer))
				}
			}
			fwdHeader, ok := message.GetFwdFrom()
			if !ok {
				return intToStringOmitZero(tgutil.ChatIdFromPeer(message.GetPeerID()))
			}
			fwdFrom, ok := fwdHeader.GetFromID()
			if !ok {
				return intToStringOmitZero(tgutil.ChatIdFromPeer(message.GetPeerID()))
			}
			return intToStringOmitZero(tgutil.ChatIdFromPeer(fwdFrom))
		}(),
	}.ToMap()
	return data
}

func intToStringOmitZero(i int64) string {
	if i == 0 {
		return ""
	}
	return fmt.Sprintf("%d", i)
}
