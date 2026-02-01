package tfile

import (
	"fmt"
	"path/filepath"

	"github.com/merisssas/bot/common/utils/fsutil"
	"github.com/merisssas/bot/config"
	"github.com/merisssas/bot/core"
	"github.com/merisssas/bot/pkg/enums/tasktype"
	"github.com/merisssas/bot/pkg/tfile"
	"github.com/merisssas/bot/storage"
)

var _ core.Executable = (*Task)(nil)

type Task struct {
	ID        string
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
	file tfile.TGFile,
	stor storage.Storage,
	path string,
	progress ProgressTracker,
) (*Task, error) {
	_, ok := stor.(storage.StorageCannotStream)
	if !config.C().Stream || ok {
		safeName := fsutil.NormalizePathname(filepath.Base(file.Name()))
		if safeName == "" || safeName == "." {
			safeName = id
		}
		cachePath, err := filepath.Abs(filepath.Join(config.C().Temp.BasePath, fmt.Sprintf("%s_%s", id, safeName)))
		if err != nil {
			return nil, fmt.Errorf("failed to get absolute path for cache: %w", err)
		}
		tfile := &Task{
			ID:        id,
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
		File:     file,
		Storage:  stor,
		Path:     path,
		Progress: progress,
		stream:   true,
	}
	return tfileTask, nil
}
