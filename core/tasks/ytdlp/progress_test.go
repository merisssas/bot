package ytdlp

import (
	"context"
	"testing"
)

func TestProgressOnProgressWithoutStart(t *testing.T) {
	t.Parallel()

	progress := &Progress{}
	task := &Task{
		ID:       "task-id",
		URLs:     []string{"https://example.com/video"},
		StorPath: "path",
		Storage:  &MockStorage{},
	}

	defer func() {
		if recover() != nil {
			t.Fatal("OnProgress should not panic when called before OnStart")
		}
	}()

	progress.OnProgress(context.Background(), task, "Downloading...")
}
