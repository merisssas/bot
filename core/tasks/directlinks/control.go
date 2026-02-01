package directlinks

import (
	"context"
	"sync"
	"time"
)

var globalPause = newPauseController()

type pauseController struct {
	mu     sync.RWMutex
	paused bool
}

func newPauseController() *pauseController {
	return &pauseController{}
}

func (p *pauseController) Pause() {
	p.mu.Lock()
	p.paused = true
	p.mu.Unlock()
}

func (p *pauseController) Resume() {
	p.mu.Lock()
	p.paused = false
	p.mu.Unlock()
}

func (p *pauseController) Wait(ctx context.Context) error {
	for {
		p.mu.RLock()
		paused := p.paused
		p.mu.RUnlock()
		if !paused {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// PauseAll pauses all directlinks downloads globally.
func PauseAll() {
	globalPause.Pause()
}

// ResumeAll resumes all directlinks downloads globally.
func ResumeAll() {
	globalPause.Resume()
}

// Pause pauses this task.
func (t *Task) Pause() {
	t.pauseMu.Lock()
	t.paused = true
	t.pauseMu.Unlock()
}

// Resume resumes this task.
func (t *Task) Resume() {
	t.pauseMu.Lock()
	t.paused = false
	t.pauseMu.Unlock()
}

func (t *Task) waitIfPaused(ctx context.Context) error {
	for {
		if err := globalPause.Wait(ctx); err != nil {
			return err
		}
		t.pauseMu.Lock()
		paused := t.paused
		t.pauseMu.Unlock()
		if !paused {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
