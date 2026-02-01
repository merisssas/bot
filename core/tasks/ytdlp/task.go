package ytdlp

import (
	"fmt"
	"strings"

	"github.com/merisssas/bot/core"
	"github.com/merisssas/bot/pkg/enums/tasktype"
	"github.com/merisssas/bot/storage"
)

var _ core.Executable = (*Task)(nil)

type Task struct {
	ID              string
	URLs            []string
	Flags           []string
	Cookie          string
	Storage         storage.Storage
	StorPath        string
	Progress        ProgressTracker
	UploadToChat    bool
	ChatID          int64
	TelegramPremium bool
}

// Title implements core.Executable.
func (t *Task) Title() string {
	urlCount := len(t.URLs)
	storageName := sanitizeTitleSegment(t.storageLabel())
	storPath := sanitizeTitleSegment(t.storagePathLabel())
	if urlCount == 1 {
		url := sanitizeTitleSegment(t.URLs[0])
		return fmt.Sprintf("%s: %s -> %s:%s", t.Type(), url, storageName, storPath)
	}
	return fmt.Sprintf("%s: %d URLs -> %s:%s", t.Type(), urlCount, storageName, storPath)
}

// Type implements core.Executable.
func (t *Task) Type() tasktype.TaskType {
	return tasktype.TaskTypeYtdlp
}

// TaskID implements core.Executable.
func (t *Task) TaskID() string {
	return t.ID
}

func NewTask(
	id string,
	urls []string,
	flags []string,
	cookie string,
	stor storage.Storage,
	storPath string,
	progressTracker ProgressTracker,
	uploadToChat bool,
	chatID int64,
	telegramPremium bool,
) *Task {
	return &Task{
		ID:              id,
		URLs:            urls,
		Flags:           flags,
		Cookie:          cookie,
		Storage:         stor,
		StorPath:        storPath,
		Progress:        progressTracker,
		UploadToChat:    uploadToChat,
		ChatID:          chatID,
		TelegramPremium: telegramPremium,
	}
}

func (t *Task) storageLabel() string {
	if t.Storage != nil {
		return t.Storage.Name()
	}
	if t.UploadToChat {
		return "telegram"
	}
	return "unknown"
}

func (t *Task) storagePathLabel() string {
	if t.StorPath != "" {
		return t.StorPath
	}
	if t.UploadToChat {
		return fmt.Sprintf("chat:%d", t.ChatID)
	}
	return "-"
}

func sanitizeTitleSegment(value string) string {
	replacer := strings.NewReplacer("\n", " ", "\r", " ", "\t", " ")
	return strings.TrimSpace(replacer.Replace(value))
}
