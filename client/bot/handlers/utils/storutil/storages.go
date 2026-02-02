package storutil

import (
	"context"

	"github.com/charmbracelet/log"
	"github.com/merisssas/Bot/database"
	storenum "github.com/merisssas/Bot/pkg/enums/storage"
	"github.com/merisssas/Bot/storage"
)

// GetUserStoragesWithTelegram returns user storages and adds the Telegram download storage
// when the user has not disabled it and no Telegram storage is configured.
func GetUserStoragesWithTelegram(ctx context.Context, chatID int64) []storage.Storage {
	stors := storage.GetUserStorages(ctx, chatID)
	user, err := database.GetUserByChatID(ctx, chatID)
	if err != nil {
		log.FromContext(ctx).Warnf("Failed to get user for storage list: %v", err)
		return stors
	}
	if user.TelegramDownloadDisabled {
		return stors
	}
	for _, stor := range stors {
		if stor.Type() == storenum.Telegram || stor.Name() == storage.TelegramDownloadStorageName {
			return stors
		}
	}
	tgStor, err := storage.GetTelegramDownloadStorage(ctx, chatID)
	if err != nil {
		log.FromContext(ctx).Warnf("Failed to initialize Telegram download storage: %v", err)
		return stors
	}
	return append(stors, tgStor)
}
