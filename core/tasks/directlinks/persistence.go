package directlinks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/log"

	"github.com/merisssas/bot/core"
	"github.com/merisssas/bot/storage"
)

const directLinksStateDir = "data/directlinks"

type persistedTask struct {
	ID          string    `json:"id"`
	Links       []string  `json:"links"`
	StorageName string    `json:"storage_name"`
	StoragePath string    `json:"storage_path"`
	MsgID       int       `json:"msg_id,omitempty"`
	UserID      int64     `json:"user_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func newPersistedTask(id string, links []string, storageName, storagePath string, tracker ProgressTracker) *persistedTask {
	persisted := &persistedTask{
		ID:          id,
		Links:       append([]string(nil), links...),
		StorageName: storageName,
		StoragePath: storagePath,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if progress, ok := tracker.(*Progress); ok {
		if cb, ok := progress.callback.(*telegramProgressCallback); ok {
			persisted.MsgID = cb.msgID
			persisted.UserID = cb.chatID
		}
	}
	return persisted
}

func (t *Task) persistState(ctx context.Context) error {
	if t.persisted == nil {
		return nil
	}
	t.persisted.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(t.persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal task %s: %w", t.ID, err)
	}
	if err := os.MkdirAll(directLinksStateDir, 0o755); err != nil {
		return fmt.Errorf("failed to create task state dir: %w", err)
	}

	dst := persistedTaskPath(t.ID)
	tmpFile, err := os.CreateTemp(directLinksStateDir, fmt.Sprintf("tmp_%s_*.json", t.ID))
	if err != nil {
		return fmt.Errorf("temp file creation failed: %w", err)
	}
	tmpName := tmpFile.Name()
	failed := true
	closed := false
	defer func() {
		if !closed {
			if err := tmpFile.Close(); err != nil {
				log.FromContext(ctx).Warnf("Failed to close temp state file %s: %v", tmpName, err)
			}
		}
		if failed {
			if err := os.Remove(tmpName); err != nil && !os.IsNotExist(err) {
				log.FromContext(ctx).Warnf("Failed to cleanup temp state file %s: %v", tmpName, err)
			}
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("fsync failed: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		closed = true
		return fmt.Errorf("close failed: %w", err)
	}
	closed = true
	if err := os.Rename(tmpName, dst); err != nil {
		return fmt.Errorf("atomic rename failed: %w", err)
	}
	failed = false
	return nil
}

func (t *Task) removeState(ctx context.Context) {
	if t == nil || t.persisted == nil {
		return
	}
	if err := os.Remove(persistedTaskPath(t.ID)); err != nil && !os.IsNotExist(err) {
		log.FromContext(ctx).Warnf("Failed to remove task state for %s: %v", t.ID, err)
	}
}

func persistedTaskPath(id string) string {
	return filepath.Join(directLinksStateDir, fmt.Sprintf("%s.json", id))
}

func RestorePersistedTasks(ctx context.Context) {
	logger := log.FromContext(ctx)
	entries, err := os.ReadDir(directLinksStateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		logger.Warnf("Failed to read directlinks state dir: %v", err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || strings.HasPrefix(entry.Name(), "tmp_") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(directLinksStateDir, entry.Name()))
		if err != nil {
			logger.Warnf("Failed to read task state %s: %v", entry.Name(), err)
			continue
		}
		var state persistedTask
		if err := json.Unmarshal(data, &state); err != nil {
			logger.Warnf("Failed to parse task state %s: %v", entry.Name(), err)
			continue
		}
		stor, err := storage.GetStorageByName(ctx, state.StorageName)
		if err != nil {
			logger.Warnf("Failed to load storage %s for task %s: %v", state.StorageName, state.ID, err)
			continue
		}

		var progress ProgressTracker
		if state.MsgID != 0 && state.UserID != 0 {
			progress = NewTelegramProgress(state.MsgID, state.UserID)
		}
		task := NewTask(state.ID, ctx, state.Links, stor, state.StoragePath, progress)
		if err := core.AddTask(ctx, task); err != nil {
			logger.Warnf("Failed to restore directlinks task %s: %v", state.ID, err)
		}
	}
}
