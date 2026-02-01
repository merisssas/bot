package aria2dl

import (
	"context"
	"fmt"
	"time"

	"github.com/merisssas/bot/config"
	"github.com/merisssas/bot/core"
	"github.com/merisssas/bot/pkg/aria2"
	"github.com/merisssas/bot/pkg/enums/tasktype"
	"github.com/merisssas/bot/storage"
)

var _ core.Executable = (*Task)(nil)

// Task represents an active Aria2 download operation.
type Task struct {
	ID  string
	GID string

	URIs []string

	Aria2Client Aria2Client
	Storage     storage.Storage
	StorPath    string
	Progress    ProgressTracker

	pollInterval time.Duration
	idleTimeout  time.Duration
}

// Title implements core.Executable.
func (t *Task) Title() string {
	return fmt.Sprintf("[%s] GID:%s ➔ %s", t.Type(), t.GID, t.Storage.Name())
}

// Type implements core.Executable.
func (t *Task) Type() tasktype.TaskType {
	return tasktype.TaskTypeAria2
}

// TaskID implements core.Executable.
func (t *Task) TaskID() string {
	return t.ID
}

func NewTask(
	id string,
	gid string,
	uris []string,
	aria2Client Aria2Client,
	stor storage.Storage,
	storPath string,
	progressTracker ProgressTracker,
) *Task {
	pollInterval := 2 * time.Second
	if config.C().Aria2.PollIntervalSeconds > 0 {
		pollInterval = time.Duration(config.C().Aria2.PollIntervalSeconds) * time.Second
	}

	idleTimeout := 30 * time.Minute
	if config.C().Aria2.IdleTimeoutMinutes > 0 {
		idleTimeout = time.Duration(config.C().Aria2.IdleTimeoutMinutes) * time.Minute
	}

	return &Task{
		ID:           id,
		GID:          gid,
		URIs:         uris,
		Aria2Client:  aria2Client,
		Storage:      stor,
		StorPath:     storPath,
		Progress:     progressTracker,
		pollInterval: pollInterval,
		idleTimeout:  idleTimeout,
	}
}

// Aria2Client defines the contract needed for the downloader.
type Aria2Client interface {
	TellStatus(ctx context.Context, gid string, keys ...string) (*aria2.Status, error)
	TellActive(ctx context.Context, keys ...string) ([]aria2.Status, error)
	TellWaiting(ctx context.Context, offset, num int, keys ...string) ([]aria2.Status, error)
	TellStopped(ctx context.Context, offset, num int, keys ...string) ([]aria2.Status, error)
	Pause(ctx context.Context, gid string) (string, error)
	Unpause(ctx context.Context, gid string) (string, error)
	GetOption(ctx context.Context, gid string) (aria2.Options, error)
	GetGlobalOption(ctx context.Context) (aria2.Options, error)
	Remove(ctx context.Context, gid string) (string, error)
	ForceRemove(ctx context.Context, gid string) (string, error)
	RemoveDownloadResult(ctx context.Context, gid string) (string, error)
}
