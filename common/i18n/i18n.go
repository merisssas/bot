// [TODO] complete the i18n support

package i18n

import (
	"embed"
	"fmt"
	"sync"

	"maps"

	"github.com/goccy/go-yaml"
	"github.com/merisssas/bot/common/i18n/i18nk"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locale/*
var localesFS embed.FS

var (
	bundle    *i18n.Bundle
	localizer *i18n.Localizer
	i18nMu    sync.RWMutex
)

func Init(lang string) error {
	i18nMu.Lock()
	defer i18nMu.Unlock()
	bundle = i18n.NewBundle(language.SimplifiedChinese)
	bundle.RegisterUnmarshalFunc("yaml", yaml.Unmarshal)
	files, err := localesFS.ReadDir("locale")
	if err != nil {
		return fmt.Errorf("failed to read locale directory: %w", err)
	}
	for _, file := range files {
		if _, err := bundle.LoadMessageFileFS(localesFS, "locale/"+file.Name()); err != nil {
			return fmt.Errorf("failed to load message file %s: %w", file.Name(), err)
		}
	}
	if lang == "" {
		lang = "zh-Hans"
	}
	localizer = i18n.NewLocalizer(bundle, lang)
	if localizer == nil {
		return fmt.Errorf("failed to create localizer, check your config for valid language setting")
	}
	return nil
}

func T(key i18nk.Key, templateData ...map[string]any) string {
	i18nMu.RLock()
	ready := localizer != nil && bundle != nil
	i18nMu.RUnlock()
	if !ready {
		if err := Init("zh-Hans"); err != nil {
			return string(key)
		}
	}
	i18nMu.RLock()
	templateDataMap := make(map[string]any)
	for _, data := range templateData {
		maps.Copy(templateDataMap, data)
	}
	msg, err := localizer.Localize(&i18n.LocalizeConfig{
		MessageID:    string(key),
		TemplateData: templateDataMap,
	})
	i18nMu.RUnlock()
	if err != nil {
		return string(key)
	}
	return msg
}
