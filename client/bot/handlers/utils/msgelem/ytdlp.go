package msgelem

import (
	"fmt"

	"github.com/gotd/td/tg"
	"github.com/rs/xid"

	"github.com/merisssas/bot/common/cache"
	"github.com/merisssas/bot/pkg/tcbdata"
)

func BuildYtdlpQualityKeyboard(choices []tcbdata.YtdlpQualityChoice) (*tg.ReplyInlineMarkup, error) {
	if len(choices) == 0 {
		return nil, fmt.Errorf("no yt-dlp quality choices provided")
	}
	buttons := make([]tg.KeyboardButtonClass, 0, len(choices))
	for _, choice := range choices {
		dataid := xid.New().String()
		if err := cache.Set(dataid, choice); err != nil {
			return nil, err
		}
		buttons = append(buttons, &tg.KeyboardButtonCallback{
			Text: choice.Label,
			Data: fmt.Appendf(nil, "%s %s", tcbdata.TypeYtdlpQual, dataid),
		})
	}
	markup := &tg.ReplyInlineMarkup{}
	row := tg.KeyboardButtonRow{Buttons: buttons}
	markup.Rows = append(markup.Rows, row)
	return markup, nil
}

func AppendYtdlpUploadButton(markup *tg.ReplyInlineMarkup, label string, data tcbdata.YtdlpSend) (*tg.ReplyInlineMarkup, error) {
	if markup == nil {
		markup = &tg.ReplyInlineMarkup{}
	}
	dataid := xid.New().String()
	if err := cache.Set(dataid, data); err != nil {
		return nil, err
	}
	markup.Rows = append(markup.Rows, tg.KeyboardButtonRow{
		Buttons: []tg.KeyboardButtonClass{
			&tg.KeyboardButtonCallback{
				Text: label,
				Data: fmt.Appendf(nil, "%s %s", tcbdata.TypeYtdlpSend, dataid),
			},
		},
	})
	return markup, nil
}
