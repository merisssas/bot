package tfile

type TaskInfo interface {
	TaskID() string
	FileName() string
	FileSize() int64
	StoragePath() string
	StorageName() string
}

type taskInfoOverride struct {
	task        *Task
	storagePath string
}

func (t *Task) TaskID() string {
	return t.ID
}

func (t *Task) FileName() string {
	return t.File.Name()
}

func (t *Task) FileSize() int64 {
	return t.File.Size()
}

func (t *Task) StoragePath() string {
	return t.Path
}

func (t *Task) StorageName() string {
	return t.Storage.Name()
}

func (t *taskInfoOverride) TaskID() string {
	return t.task.TaskID()
}

func (t *taskInfoOverride) FileName() string {
	return t.task.FileName()
}

func (t *taskInfoOverride) FileSize() int64 {
	return t.task.FileSize()
}

func (t *taskInfoOverride) StoragePath() string {
	return t.storagePath
}

func (t *taskInfoOverride) StorageName() string {
	return t.task.StorageName()
}
