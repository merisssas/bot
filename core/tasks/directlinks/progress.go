package directlinks

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/log"
)

type TaskInfo interface {
	TotalBytes() int64
	TotalFiles() int
	TaskID() string
	StorageName() string
	StoragePath() string
	DownloadedBytes() int64
	Processing() []FileInfo
}

type FileInfo interface {
	FileName() string
	FileSize() int64
}

type ProgressTracker interface {
	OnStart(ctx context.Context, info TaskInfo)
	OnProgress(ctx context.Context, info TaskInfo)
	OnDone(ctx context.Context, info TaskInfo, err error)
}

type Progress struct {
	start             time.Time
	lastUpdate        atomic.Value // time.Time
	lastUpdatePercent atomic.Int32
	minUpdateInterval time.Duration
	callback          ProgressCallback
	speedMu           sync.Mutex
	lastSpeedBytes    int64
	lastSpeedTime     time.Time
	emaSpeed          float64
}

type ProgressSnapshot struct {
	TotalBytes      int64
	TotalFiles      int
	DownloadedBytes int64
	Percent         int
	Speed           float64
	Remaining       time.Duration
	Processing      []FileInfo
}

type ProgressCallback interface {
	OnStart(ctx context.Context, info TaskInfo, snapshot ProgressSnapshot)
	OnProgress(ctx context.Context, info TaskInfo, snapshot ProgressSnapshot)
	OnDone(ctx context.Context, info TaskInfo, snapshot ProgressSnapshot, err error)
}

// OnDone implements ProgressTracker.
func (p *Progress) OnDone(ctx context.Context, info TaskInfo, err error) {
	if p.callback == nil {
		return
	}
	p.callback.OnDone(ctx, info, p.buildSnapshot(info), err)
}

// OnProgress implements ProgressTracker.
func (p *Progress) OnProgress(ctx context.Context, info TaskInfo) {
	if !shouldUpdateProgress(info.TotalBytes(), info.DownloadedBytes(), int(p.lastUpdatePercent.Load())) {
		return
	}
	percent := int((info.DownloadedBytes() * 100) / info.TotalBytes())
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	if p.lastUpdatePercent.Load() == int32(percent) {
		return
	}
	lastUpdate := p.lastUpdate.Load()
	if lastUpdate != nil {
		lastTime := lastUpdate.(time.Time)
		if time.Since(lastTime) < p.minUpdateInterval {
			return
		}
	}
	p.lastUpdatePercent.Store(int32(percent))
	p.lastUpdate.Store(time.Now())
	log.FromContext(ctx).Debugf("Progress update: %s, %d/%d", info.TaskID(), info.DownloadedBytes(), info.TotalBytes())
	if p.callback == nil {
		return
	}
	p.callback.OnProgress(ctx, info, p.buildSnapshot(info))
}

// OnStart implements ProgressTracker.
func (p *Progress) OnStart(ctx context.Context, info TaskInfo) {
	p.start = time.Now()
	p.lastUpdatePercent.Store(0)
	p.lastUpdate.Store(time.Now())
	p.minUpdateInterval = 2 * time.Second
	p.speedMu.Lock()
	p.lastSpeedBytes = info.DownloadedBytes()
	p.lastSpeedTime = time.Now()
	p.emaSpeed = 0
	p.speedMu.Unlock()
	if p.callback == nil {
		return
	}
	p.callback.OnStart(ctx, info, p.buildSnapshot(info))
}

var _ ProgressTracker = (*Progress)(nil)

func NewProgress(callback ProgressCallback) ProgressTracker {
	return &Progress{
		callback: callback,
	}
}

func (p *Progress) buildSnapshot(info TaskInfo) ProgressSnapshot {
	downloaded := info.DownloadedBytes()
	total := info.TotalBytes()
	speed := p.currentSpeed(downloaded)
	remaining := time.Duration(0)
	if speed > 0 && total > downloaded {
		remaining = time.Duration(float64(total-downloaded)/speed) * time.Second
	}
	percent := 0
	if total > 0 {
		percent = int((downloaded * 100) / total)
		if percent < 0 {
			percent = 0
		}
		if percent > 100 {
			percent = 100
		}
	}
	return ProgressSnapshot{
		TotalBytes:      total,
		TotalFiles:      info.TotalFiles(),
		DownloadedBytes: downloaded,
		Percent:         percent,
		Speed:           speed,
		Remaining:       remaining,
		Processing:      info.Processing(),
	}
}

func (p *Progress) currentSpeed(downloaded int64) float64 {
	p.speedMu.Lock()
	defer p.speedMu.Unlock()

	now := time.Now()
	if p.lastSpeedTime.IsZero() {
		p.lastSpeedTime = now
		p.lastSpeedBytes = downloaded
		return 0
	}

	elapsed := now.Sub(p.lastSpeedTime).Seconds()
	if elapsed <= 0 {
		return p.emaSpeed
	}

	delta := downloaded - p.lastSpeedBytes
	if delta < 0 {
		delta = 0
	}

	instant := float64(delta) / elapsed
	const smoothing = 0.3
	if p.emaSpeed == 0 {
		p.emaSpeed = instant
	} else {
		p.emaSpeed = smoothing*instant + (1-smoothing)*p.emaSpeed
	}

	p.lastSpeedBytes = downloaded
	p.lastSpeedTime = now

	return p.emaSpeed
}

func renderProgressBar(percent int) string {
	const width = 12
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := int((float64(percent) / 100.0) * float64(width))
	empty := width - filled
	return strings.Repeat("▓", filled) + strings.Repeat("░", empty)
}
