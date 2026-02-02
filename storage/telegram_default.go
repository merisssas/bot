package storage

import (
	"context"
	"fmt"
	"sync"

	storconfig "github.com/merisssas/Bot/config/storage"
)

const TelegramDownloadStorageName = "Telegram (Chat)"

var telegramDownloadStorages sync.Map

// GetTelegramDownloadStorage returns a per-user Telegram storage that sends files to the user's chat.
func GetTelegramDownloadStorage(ctx context.Context, chatID int64) (Storage, error) {
	if chatID <= 0 {
		return nil, fmt.Errorf("invalid chat ID: %d", chatID)
	}
	if stor, ok := telegramDownloadStorages.Load(chatID); ok {
		return stor.(Storage), nil
	}
	cfg := &storconfig.TelegramStorageConfig{
		BaseConfig: storconfig.BaseConfig{
			Name:   TelegramDownloadStorageName,
			Enable: true,
		},
		ChatID: chatID,
	}
	stor, err := NewStorage(ctx, cfg)
	if err != nil {
		return nil, err
	}
	telegramDownloadStorages.Store(chatID, stor)
	return stor, nil
}
