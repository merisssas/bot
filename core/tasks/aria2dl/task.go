package aria2dl

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/merisssas/Bot/core"
	"github.com/merisssas/Bot/pkg/aria2"
	"github.com/merisssas/Bot/pkg/enums/tasktype"
	"github.com/merisssas/Bot/storage"
)

var _ core.Executable = (*Task)(nil)

type TaskConfig struct {
	MaxRetries          int               `json:"max_retries"`
	RetryBaseDelay      time.Duration     `json:"retry_base_delay"`
	RetryMaxDelay       time.Duration     `json:"retry_max_delay"`
	Priority            int               `json:"priority"`
	VerifyHash          bool              `json:"verify_hash"`
	HashType            string            `json:"hash_type,omitempty"`
	ExpectedHash        string            `json:"expected_hash,omitempty"`
	CustomHeaders       map[string]string `json:"custom_headers,omitempty"`
	ProxyURL            string            `json:"proxy_url,omitempty"`
	LimitRate           string            `json:"limit_rate,omitempty"`
	BurstRate           string            `json:"burst_rate,omitempty"`
	BurstDuration       time.Duration     `json:"burst_duration,omitempty"`
	UserAgent           string            `json:"user_agent,omitempty"`
	EnableResume        bool              `json:"enable_resume"`
	Split               int               `json:"split"`
	MaxConnPerServer    int               `json:"max_conn_per_server"`
	MinSplitSize        string            `json:"min_split_size,omitempty"`
	OverwritePolicy     string            `json:"overwrite_policy,omitempty"`
	DryRun              bool              `json:"dry_run"`
	ChecksumAlgorithm   string            `json:"checksum_algorithm,omitempty"`
	ExpectedChecksum    string            `json:"expected_checksum,omitempty"`
	RequireChecksum     bool              `json:"require_checksum,omitempty"`
	EnableIntegrityScan bool              `json:"enable_integrity_scan,omitempty"`
}

type TaskStats struct {
	TotalSize    int64         `json:"total_size"`
	Downloaded   int64         `json:"downloaded"`
	Speed        int64         `json:"speed"`
	ETA          time.Duration `json:"eta"`
	Connections  int           `json:"connections"`
	ProgressPct  float64       `json:"progress_pct"`
	StartTime    time.Time     `json:"start_time"`
	CompleteTime time.Time     `json:"complete_time,omitempty"`
	LastUpdate   time.Time     `json:"last_update"`
	RetryCount   int           `json:"retry_count"`
	LastError    string        `json:"last_error,omitempty"`
	IsStalled    bool          `json:"is_stalled"`
}

type Task struct {
	ID  string `json:"id"`
	gid string

	ctx    context.Context    `json:"-"`
	cancel context.CancelFunc `json:"-"`

	URIs        []string        `json:"uris"`
	Aria2Client *aria2.Client   `json:"-"`
	Storage     storage.Storage `json:"-"`
	StorPath    string          `json:"stor_path"`
	Progress    ProgressTracker `json:"-"`

	Config TaskConfig   `json:"config"`
	Stats  TaskStats    `json:"stats"`
	mu     sync.RWMutex `json:"-"`
}

type Option func(*Task)

func WithPriority(priority int) Option {
	return func(t *Task) {
		t.Config.Priority = priority
	}
}

func WithChecksum(algo, hash string) Option {
	return func(t *Task) {
		t.Config.VerifyHash = true
		t.Config.HashType = algo
		t.Config.ExpectedHash = hash
	}
}

func WithMaxRetries(maxRetries int) Option {
	return func(t *Task) {
		t.Config.MaxRetries = maxRetries
	}
}

func WithHeaders(headers map[string]string) Option {
	return func(t *Task) {
		t.Config.CustomHeaders = headers
	}
}

func WithProxy(proxyURL string) Option {
	return func(t *Task) {
		t.Config.ProxyURL = proxyURL
	}
}

func WithLimit(bytesPerSec int64) Option {
	return func(t *Task) {
		t.Config.LimitRate = fmt.Sprintf("%d", bytesPerSec)
	}
}

func WithTaskConfig(config TaskConfig) Option {
	return func(t *Task) {
		t.Config = config
	}
}

// Title implements core.Executable.
func (t *Task) Title() string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	prioLabels := []string{"LOW", "NRM", "HIGH", "CRIT"}
	prioStr := "NRM"
	if t.Config.Priority >= 0 && t.Config.Priority < len(prioLabels) {
		prioStr = prioLabels[t.Config.Priority]
	}

	proxyIcon := ""
	if t.Config.ProxyURL != "" {
		proxyIcon = "🛡️ "
	}

	return fmt.Sprintf("[%s%s][%s] %s -> %s", proxyIcon, t.Type(), prioStr, t.gid, t.Storage.Name())
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
	opts ...Option,
) *Task {
	ctx, cancel := context.WithCancel(ctx)

	task := &Task{
		ID:          id,
		ctx:         ctx,
		cancel:      cancel,
		gid:         gid,
		URIs:        uris,
		Aria2Client: aria2Client,
		Storage:     stor,
		StorPath:    storPath,
		Progress:    progressTracker,
		Config:      DefaultTaskConfig(),
		Stats: TaskStats{
			StartTime: time.Now(),
		},
	}

	for _, opt := range opts {
		opt(task)
	}

	return task
}

func (t *Task) GID() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.gid
}

func (t *Task) SetGID(newGID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.gid = newGID
}

func (t *Task) UpdateStats(downloaded, total, speed int64, connections int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	lastUpdate := t.Stats.LastUpdate
	t.Stats.Downloaded = downloaded
	t.Stats.TotalSize = total
	t.Stats.Speed = speed
	t.Stats.Connections = connections
	t.Stats.LastUpdate = now

	if total > 0 {
		t.Stats.ProgressPct = (float64(downloaded) / float64(total)) * 100
		remaining := total - downloaded
		if speed > 0 {
			t.Stats.ETA = time.Duration(remaining/speed) * time.Second
			t.Stats.IsStalled = false
		} else if !lastUpdate.IsZero() && now.Sub(lastUpdate) > 30*time.Second {
			t.Stats.IsStalled = true
		}
	}
}

func (t *Task) Snapshot() TaskStats {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Stats
}

func (t *Task) SetError(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err != nil {
		t.Stats.LastError = err.Error()
	}
}

func (t *Task) IncrementRetry() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Stats.RetryCount++
	return t.Stats.RetryCount
}

func (t *Task) Cancel() {
	if t.cancel == nil {
		return
	}
	t.cancel()
}

func (t *Task) IsHealthy() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if time.Since(t.Stats.StartTime) > time.Minute && t.Stats.Downloaded == 0 {
		return false
	}
	return !t.Stats.IsStalled
}
