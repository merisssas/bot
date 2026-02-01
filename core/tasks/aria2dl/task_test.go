package aria2dl

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	storconfig "github.com/merisssas/bot/config/storage"
	"github.com/merisssas/bot/pkg/aria2"
	storenum "github.com/merisssas/bot/pkg/enums/storage"
	"github.com/merisssas/bot/pkg/enums/tasktype"
)

type mockStorage struct {
	mu       sync.Mutex
	name     string
	savePath []string
}

func (m *mockStorage) Name() string {
	return m.name
}

func (m *mockStorage) Type() storenum.StorageType {
	return storenum.StorageType("mock")
}

func (m *mockStorage) Init(ctx context.Context, config storconfig.StorageConfig) error {
	return nil
}

func (m *mockStorage) Save(ctx context.Context, reader io.Reader, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.savePath = append(m.savePath, path)
	return nil
}

func (m *mockStorage) Exists(ctx context.Context, path string) bool {
	return false
}

type mockProgress struct {
	started  bool
	done     bool
	doneErr  error
	progress int
}

func (m *mockProgress) OnStart(ctx context.Context, task *Task) {
	m.started = true
}

func (m *mockProgress) OnProgress(ctx context.Context, task *Task, status *aria2.Status) {
	m.progress++
}

func (m *mockProgress) OnDone(ctx context.Context, task *Task, err error) {
	m.done = true
	m.doneErr = err
}

type mockAria2Client struct {
	mu              sync.Mutex
	statuses        map[string][]*aria2.Status
	statusIndex     map[string]int
	tellStatusErr   map[string]error
	waitingStatuses []aria2.Status
	stoppedStatuses []aria2.Status
	removed         []string
	forceRemoved    []string
	removedResults  []string
}

func (m *mockAria2Client) TellStatus(ctx context.Context, gid string, keys ...string) (*aria2.Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.tellStatusErr[gid]; ok {
		return nil, err
	}
	statuses, ok := m.statuses[gid]
	if !ok || len(statuses) == 0 {
		return nil, errors.New("status not found")
	}
	idx := m.statusIndex[gid]
	if idx >= len(statuses) {
		idx = len(statuses) - 1
	}
	m.statusIndex[gid] = idx + 1
	return statuses[idx], nil
}

func (m *mockAria2Client) TellWaiting(ctx context.Context, offset, num int, keys ...string) ([]aria2.Status, error) {
	return sliceStatuses(m.waitingStatuses, offset, num), nil
}

func (m *mockAria2Client) TellStopped(ctx context.Context, offset, num int, keys ...string) ([]aria2.Status, error) {
	return sliceStatuses(m.stoppedStatuses, offset, num), nil
}

func (m *mockAria2Client) Pause(ctx context.Context, gid string) (string, error) {
	return "OK", nil
}

func (m *mockAria2Client) Unpause(ctx context.Context, gid string) (string, error) {
	return "OK", nil
}

func (m *mockAria2Client) GetOption(ctx context.Context, gid string) (aria2.Options, error) {
	return aria2.Options{}, nil
}

func (m *mockAria2Client) Remove(ctx context.Context, gid string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removed = append(m.removed, gid)
	return "OK", nil
}

func (m *mockAria2Client) ForceRemove(ctx context.Context, gid string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.forceRemoved = append(m.forceRemoved, gid)
	return "OK", nil
}

func (m *mockAria2Client) RemoveDownloadResult(ctx context.Context, gid string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removedResults = append(m.removedResults, gid)
	return "OK", nil
}

func sliceStatuses(statuses []aria2.Status, offset, num int) []aria2.Status {
	if offset < 0 {
		offset = 0
	}
	if num <= 0 {
		return nil
	}
	if offset >= len(statuses) {
		return nil
	}
	end := offset + num
	if end > len(statuses) {
		end = len(statuses)
	}
	return statuses[offset:end]
}

func TestTaskCreation(t *testing.T) {
	mockStor := &mockStorage{name: "test-storage"}
	mockProg := &mockProgress{}

	task := NewTask(
		"test-task-id",
		"test-gid",
		[]string{"http://example.com/file.zip"},
		nil,
		mockStor,
		"/test/path",
		mockProg,
	)

	if task.ID != "test-task-id" {
		t.Errorf("Expected task ID to be 'test-task-id', got '%s'", task.ID)
	}

	if task.GID != "test-gid" {
		t.Errorf("Expected GID to be 'test-gid', got '%s'", task.GID)
	}

	if task.Type() != tasktype.TaskTypeAria2 {
		t.Errorf("Expected task type to be TaskTypeAria2, got '%s'", task.Type())
	}

	if task.TaskID() != "test-task-id" {
		t.Errorf("Expected TaskID() to return 'test-task-id', got '%s'", task.TaskID())
	}

	if task.Storage.Name() != "test-storage" {
		t.Errorf("Expected storage name to be 'test-storage', got '%s'", task.Storage.Name())
	}
}

func TestProgressTracker(t *testing.T) {
	ctx := context.Background()
	mockStor := &mockStorage{name: "test-storage"}
	mockProg := &mockProgress{}

	task := NewTask(
		"test-task-id",
		"test-gid",
		[]string{"http://example.com/file.zip"},
		nil,
		mockStor,
		"/test/path",
		mockProg,
	)

	mockProg.OnStart(ctx, task)
	if !mockProg.started {
		t.Error("Expected OnStart to set started to true")
	}

	status := &aria2.Status{
		GID:             "test-gid",
		Status:          "active",
		TotalLength:     "1000000",
		CompletedLength: "500000",
		DownloadSpeed:   "100000",
	}
	mockProg.OnProgress(ctx, task, status)
	if mockProg.progress != 1 {
		t.Errorf("Expected progress to be 1, got %d", mockProg.progress)
	}

	mockProg.OnDone(ctx, task, nil)
	if !mockProg.done {
		t.Error("Expected OnDone to set done to true")
	}
	if mockProg.doneErr != nil {
		t.Errorf("Expected doneErr to be nil, got %v", mockProg.doneErr)
	}
}

func TestTaskTitle(t *testing.T) {
	mockStor := &mockStorage{name: "test-storage"}

	task := NewTask(
		"test-task-id",
		"test-gid-123",
		[]string{"http://example.com/file.zip"},
		nil,
		mockStor,
		"/test/path",
		nil,
	)

	title := task.Title()
	expectedSubstr := "test-gid-123"
	if len(title) == 0 {
		t.Error("Expected title to not be empty")
	}

	found := false
	for i := 0; i < len(title)-len(expectedSubstr)+1; i++ {
		if title[i:i+len(expectedSubstr)] == expectedSubstr {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected title to contain GID '%s', got '%s'", expectedSubstr, title)
	}
}

func TestExecuteFailsWithNilClient(t *testing.T) {
	ctx := context.Background()
	mockStor := &mockStorage{name: "test-storage"}
	task := NewTask(
		"test-task-id",
		"test-gid",
		[]string{"http://example.com/file.zip"},
		nil,
		mockStor,
		"/test/path",
		nil,
	)

	if err := task.Execute(ctx); err == nil {
		t.Error("Expected Execute to fail when aria2 client is nil")
	}
}

func TestWaitForDownloadFollowedByMultiple(t *testing.T) {
	client := &mockAria2Client{
		statuses: map[string][]*aria2.Status{
			"meta": {
				{GID: "meta", Status: "complete", FollowedBy: []string{"gid1", "gid2"}},
			},
			"gid1": {
				{GID: "gid1", Status: "active"},
				{GID: "gid1", Status: "complete"},
			},
			"gid2": {
				{GID: "gid2", Status: "complete"},
			},
		},
		statusIndex: make(map[string]int),
	}

	task := NewTask(
		"test-task-id",
		"meta",
		[]string{"magnet:?xt=urn:btih:hash"},
		client,
		&mockStorage{name: "test-storage"},
		"/test/path",
		nil,
	)
	task.pollInterval = 10 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	completed, err := task.waitForDownload(ctx)
	if err != nil {
		t.Fatalf("Expected waitForDownload to succeed, got error: %v", err)
	}
	if len(completed) != 3 {
		t.Fatalf("Expected 3 completed GIDs, got %d", len(completed))
	}
}

func TestGetStatusUsesPaginationForStoppedQueue(t *testing.T) {
	stopped := make([]aria2.Status, 0, 150)
	for i := 0; i < 150; i++ {
		stopped = append(stopped, aria2.Status{GID: "gid-" + strconv.Itoa(i), Status: "complete"})
	}
	stopped[120].GID = "target"
	client := &mockAria2Client{
		stoppedStatuses: stopped,
		tellStatusErr:   map[string]error{"target": errors.New("not active")},
	}

	task := NewTask(
		"test-task-id",
		"target",
		nil,
		client,
		&mockStorage{name: "test-storage"},
		"/test/path",
		nil,
	)

	status, err := task.getStatus(context.Background(), "target")
	if err != nil {
		t.Fatalf("Expected status to be found in stopped queue, got error: %v", err)
	}
	if status.GID != "target" {
		t.Fatalf("Expected GID 'target', got '%s'", status.GID)
	}
}

func TestTransferFilesPreservesStructure(t *testing.T) {
	baseDir := t.TempDir()
	fileA := filepath.Join(baseDir, "dir-a", "file.txt")
	fileB := filepath.Join(baseDir, "dir-b", "file.txt")

	if err := os.MkdirAll(filepath.Dir(fileA), 0o755); err != nil {
		t.Fatalf("Failed to create dir-a: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(fileB), 0o755); err != nil {
		t.Fatalf("Failed to create dir-b: %v", err)
	}
	if err := os.WriteFile(fileA, []byte("a"), 0o644); err != nil {
		t.Fatalf("Failed to write fileA: %v", err)
	}
	if err := os.WriteFile(fileB, []byte("b"), 0o644); err != nil {
		t.Fatalf("Failed to write fileB: %v", err)
	}

	client := &mockAria2Client{
		statuses: map[string][]*aria2.Status{
			"gid": {
				{
					GID:    "gid",
					Status: "complete",
					Dir:    baseDir,
					Files: []aria2.File{
						{Path: fileA, Selected: "true"},
						{Path: fileB, Selected: "true"},
					},
				},
			},
		},
		statusIndex: make(map[string]int),
	}

	stor := &mockStorage{name: "test-storage"}
	task := NewTask(
		"test-task-id",
		"gid",
		nil,
		client,
		stor,
		"/dest",
		nil,
	)

	if err := task.transferFiles(context.Background(), []string{"gid"}); err != nil {
		t.Fatalf("Expected transferFiles to succeed, got error: %v", err)
	}

	stor.mu.Lock()
	defer stor.mu.Unlock()
	if len(stor.savePath) != 2 {
		t.Fatalf("Expected 2 saved paths, got %d", len(stor.savePath))
	}
	if stor.savePath[0] == stor.savePath[1] {
		t.Fatalf("Expected distinct destination paths, got %v", stor.savePath)
	}
	if filepath.Base(stor.savePath[0]) != "file.txt" || filepath.Base(stor.savePath[1]) != "file.txt" {
		t.Fatalf("Expected base names to match original file, got %v", stor.savePath)
	}
}

func TestTransferFilesMissingFileFails(t *testing.T) {
	client := &mockAria2Client{
		statuses: map[string][]*aria2.Status{
			"gid": {
				{
					GID:    "gid",
					Status: "complete",
					Dir:    t.TempDir(),
					Files: []aria2.File{
						{Path: filepath.Join(t.TempDir(), "missing.txt"), Selected: "true"},
					},
				},
			},
		},
		statusIndex: make(map[string]int),
	}

	stor := &mockStorage{name: "test-storage"}
	task := NewTask(
		"test-task-id",
		"gid",
		nil,
		client,
		stor,
		"/dest",
		nil,
	)

	if err := task.transferFiles(context.Background(), []string{"gid"}); err == nil {
		t.Fatal("Expected transferFiles to fail when source file is missing")
	}
}

func TestExecuteCancelTriggersForceRemove(t *testing.T) {
	client := &mockAria2Client{
		statuses:    make(map[string][]*aria2.Status),
		statusIndex: make(map[string]int),
	}
	stor := &mockStorage{name: "test-storage"}
	task := NewTask(
		"test-task-id",
		"gid",
		nil,
		client,
		stor,
		"/dest",
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := task.Execute(ctx); err == nil {
		t.Fatal("Expected Execute to return error on canceled context")
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.forceRemoved) == 0 {
		t.Fatal("Expected ForceRemove to be called on cancel")
	}
	if client.forceRemoved[0] != "gid" {
		t.Fatalf("Expected ForceRemove to be called with gid, got %v", client.forceRemoved)
	}
}
