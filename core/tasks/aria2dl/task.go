package aria2dl

import (
	"context"
	"fmt"

	"github.com/merisssas/Bot/core"
	"github.com/merisssas/Bot/pkg/aria2"
	"github.com/merisssas/Bot/pkg/enums/tasktype"
	"github.com/merisssas/Bot/storage"
)

var _ core.Executable = (*Task)(nil)

type Task struct {
	ID          string
	ctx         context.Context
	GID         string
	URIs        []string
	Aria2Client *aria2.Client
	Storage     storage.Storage
	StorPath    string
	Progress    ProgressTracker
}

// Title implements core.Executable.
func (t *Task) Title() string {
	return fmt.Sprintf("[%s](Aria2 GID:%s->%s:%s)", t.Type(), t.GID, t.Storage.Name(), t.StorPath)
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
	ctx context.Context,
	gid string,
	uris []string,
	aria2Client *aria2.Client,
	stor storage.Storage,
	storPath string,
	progressTracker ProgressTracker,
) *Task {
	return &Task{
		ID:          id,
		ctx:         ctx,
		GID:         gid,
		URIs:        uris,
		Aria2Client: aria2Client,
		Storage:     stor,
		StorPath:    storPath,
		Progress:    progressTracker,
	}
}
