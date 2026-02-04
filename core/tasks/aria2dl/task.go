package aria2dl

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/merisssas/Bot/core"
	"github.com/merisssas/Bot/pkg/aria2"
	"github.com/merisssas/Bot/pkg/enums/tasktype"
	"github.com/merisssas/Bot/storage"
)

var _ core.Executable = (*Task)(nil)

type TaskConfig struct {
	MaxRetries     int           `json:"max_retries"`
	RetryBaseDelay time.Duration `json:"retry_base_delay"`
	RetryMaxDelay  time.Duration `json:"retry_max_delay"`
	Priority       int           `json:"priority"`

	VerifyHash          bool   `json:"verify_hash"`
	HashType            string `json:"hash_type,omitempty"`
	ExpectedHash        string `json:"expected_hash,omitempty"`
	EnableIntegrityScan bool   `json:"enable_integrity_scan,omitempty"`

	CustomHeaders map[string]string `json:"custom_headers,omitempty"`
	ProxyURL      string            `json:"proxy_url,omitempty"`
	LimitRate     string            `json:"limit_rate,omitempty"`
	BurstRate     string            `json:"burst_rate,omitempty"`
	BurstDuration time.Duration     `json:"burst_duration,omitempty"`
	UserAgent     string            `json:"user_agent,omitempty"`

	EnableResume     bool            `json:"enable_resume"`
	Split            int             `json:"split"`
	MaxConnPerServer int             `json:"max_conn_per_server"`
	MinSplitSize     string          `json:"min_split_size,omitempty"`
	OverwritePolicy  OverwritePolicy `json:"overwrite_policy,omitempty"`
	DryRun           bool            `json:"dry_run"`
}

func (c TaskConfig) MarshalJSON() ([]byte, error) {
	type Alias TaskConfig
	aux := &struct {
		CustomHeaders map[string]string `json:"custom_headers,omitempty"`
		Alias
	}{
		Alias: (Alias)(c),
	}

	if len(c.CustomHeaders) > 0 {
		redacted := make(map[string]string, len(c.CustomHeaders))
		for key, value := range c.CustomHeaders {
			lowerKey := strings.ToLower(key)
			if lowerKey == "authorization" || lowerKey == "cookie" || strings.Contains(lowerKey, "token") {
				redacted[key] = "████████"
				continue
			}
			redacted[key] = value
		}
		aux.CustomHeaders = redacted
	}

	return json.Marshal(aux)
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

	ctx        context.Context    `json:"-"`
	cancel     context.CancelFunc `json:"-"`
	cancelOnce sync.Once          `json:"-"`

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

	indicators := make([]string, 0, 2)
	if t.Config.ProxyURL != "" {
		indicators = append(indicators, "🛡️")
	}
	if t.Config.DryRun {
		indicators = append(indicators, "🧪")
	}

	prioStr := "NRM"
	if t.Config.Priority > 0 {
		prioStr = "HIGH"
	} else if t.Config.Priority < 0 {
		prioStr = "LOW"
	}

	prefix := ""
	if len(indicators) > 0 {
		prefix = fmt.Sprintf("[%s] ", strings.Join(indicators, ""))
	}

	return fmt.Sprintf("%s[%s][%s] %s -> %s", prefix, t.Type(), prioStr, t.gid, t.Storage.Name())
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
			StartTime:  time.Now(),
			LastUpdate: time.Now(),
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
		if speed > 0 {
			remaining := total - downloaded
			t.Stats.ETA = time.Duration(remaining/speed) * time.Second
			t.Stats.IsStalled = false
		} else {
			t.Stats.ETA = -1
			if !lastUpdate.IsZero() && now.Sub(lastUpdate) > 30*time.Second {
				t.Stats.IsStalled = true
			}
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
	t.cancelOnce.Do(func() {
		if t.cancel == nil {
			return
		}
		t.cancel()
	})
}

func (t *Task) IsHealthy() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if time.Since(t.Stats.StartTime) < time.Minute {
		return true
	}

	if time.Since(t.Stats.LastUpdate) > time.Minute {
		return false
	}

	if t.Stats.IsStalled {
		return false
	}

	return true
}
