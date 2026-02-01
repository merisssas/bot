package tfile

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/merisssas/Bot/config"
	"github.com/merisssas/Bot/core"
	"github.com/merisssas/Bot/pkg/enums/tasktype"
	"github.com/merisssas/Bot/pkg/tfile"
	"github.com/merisssas/Bot/storage"
)

var _ core.Executable = (*Task)(nil)

type Task struct {
	ID        string
	Ctx       context.Context
	File      tfile.TGFile
	Storage   storage.Storage
	Path      string
	Progress  ProgressTracker
	stream    bool // true if the file should be downloaded in stream mode
	localPath string
}

// Title implements core.Exectable.
func (t *Task) Title() string {
	return fmt.Sprintf("[%s](%s->%s:%s)", t.Type(), t.File.Name(), t.Storage.Name(), t.Path)
}

func (t *Task) Type() tasktype.TaskType {
	return tasktype.TaskTypeTgfiles
}

func NewTGFileTask(
	id string,
	ctx context.Context,
	file tfile.TGFile,
	stor storage.Storage,
	path string,
	progress ProgressTracker,
) (*Task, error) {
	_, ok := stor.(storage.StorageCannotStream)
	if !config.C().Stream || ok {
		cachePath, err := filepath.Abs(filepath.Join(config.C().Temp.BasePath, fmt.Sprintf("%s_%s", id, file.Name())))
		if err != nil {
			return nil, fmt.Errorf("failed to get absolute path for cache: %w", err)
		}
		tfile := &Task{
			ID:        id,
			Ctx:       ctx,
			File:      file,
			Storage:   stor,
			Path:      path,
			Progress:  progress,
			localPath: cachePath,
		}
		return tfile, nil
	}
	tfileTask := &Task{
		ID:       id,
		Ctx:      ctx,
		File:     file,
		Storage:  stor,
		Path:     path,
		Progress: progress,
		stream:   true,
	}
	return tfileTask, nil
}
