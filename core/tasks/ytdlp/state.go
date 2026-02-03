package ytdlp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type stateManager struct {
	path          string
	flushInterval time.Duration
	mu            sync.Mutex
	lastFlush     time.Time
	state         persistedState
}

type persistedState struct {
	Version int                   `json:"version"`
	Tasks   map[string]*taskState `json:"tasks"`
	Updated time.Time             `json:"updated_at"`
}

type taskState struct {
	TaskID  string               `json:"task_id"`
	Created time.Time            `json:"created_at"`
	Updated time.Time            `json:"updated_at"`
	URLs    map[string]*urlState `json:"urls"`
}

type urlState struct {
	URL             string    `json:"url"`
	Status          string    `json:"status"`
	Filename        string    `json:"filename"`
	Percent         float64   `json:"percent"`
	DownloadedBytes int64     `json:"downloaded_bytes"`
	TotalBytes      int64     `json:"total_bytes"`
	FragmentIndex   int       `json:"fragment_index"`
	FragmentCount   int       `json:"fragment_count"`
	Proxy           string    `json:"proxy"`
	IPMode          string    `json:"ip_mode"`
	Format          string    `json:"format"`
	StartedAt       time.Time `json:"started_at"`
	LastUpdated     time.Time `json:"last_updated"`
	Completed       bool      `json:"completed"`
}

func newStateManager(path string) *stateManager {
	if path == "" {
		return nil
	}
	manager := &stateManager{
		path:          path,
		flushInterval: 2 * time.Second,
		state: persistedState{
			Version: 1,
			Tasks:   make(map[string]*taskState),
		},
	}
	_ = manager.load()
	return manager
}

func (m *stateManager) Update(taskID string, update urlState) {
	if m == nil || taskID == "" || update.URL == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	task := m.ensureTask(taskID)
	current := task.URLs[update.URL]
	if current == nil {
		current = &urlState{
			URL:       update.URL,
			StartedAt: time.Now(),
		}
	}
	update.StartedAt = current.StartedAt
	update.LastUpdated = time.Now()
	task.URLs[update.URL] = &update
	task.Updated = time.Now()
	m.state.Updated = time.Now()
	m.flushLocked()
}

func (m *stateManager) MarkComplete(taskID, url string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	task := m.ensureTask(taskID)
	state := task.URLs[url]
	if state == nil {
		state = &urlState{URL: url, StartedAt: time.Now()}
	}
	state.Completed = true
	state.LastUpdated = time.Now()
	task.URLs[url] = state
	task.Updated = time.Now()
	m.state.Updated = time.Now()
	m.flushLocked()
}

func (m *stateManager) Snapshot(taskID string) *taskState {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if task, ok := m.state.Tasks[taskID]; ok {
		return task
	}
	return nil
}

func (m *stateManager) flushLocked() {
	if time.Since(m.lastFlush) < m.flushInterval {
		return
	}
	m.lastFlush = time.Now()
	_ = m.saveLocked()
}

func (m *stateManager) ensureTask(taskID string) *taskState {
	task, ok := m.state.Tasks[taskID]
	if !ok {
		task = &taskState{
			TaskID:  taskID,
			Created: time.Now(),
			URLs:    make(map[string]*urlState),
		}
		m.state.Tasks[taskID] = task
	}
	return task
}

func (m *stateManager) load() error {
	if m == nil {
		return nil
	}
	raw, err := os.ReadFile(m.path)
	if err != nil {
		return nil
	}
	return json.Unmarshal(raw, &m.state)
}

func (m *stateManager) saveLocked() error {
	if m == nil {
		return nil
	}
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, m.path)
}
