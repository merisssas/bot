package core

import (
	"context"
	"errors"
	"runtime/debug"
	"sync"
	"time"

	"github.com/charmbracelet/log"

	"github.com/merisssas/Bot/config"
	"github.com/merisssas/Bot/pkg/enums/tasktype"
	"github.com/merisssas/Bot/pkg/queue"
)

const (
	MaxRetries       = 3
	BaseRetryBackoff = 2 * time.Second
)

var (
	ErrTaskPanic = errors.New("task panicked during execution")
)

type Executable interface {
	Type() tasktype.TaskType
	Title() string
	TaskID() string
	Execute(ctx context.Context) error
}

var (
	engine *TaskEngine
	once   sync.Once
)

type TaskEngine struct {
	queue   *queue.TaskQueue[Executable]
	logger  *log.Logger
	wg      sync.WaitGroup
	quit    chan struct{}
	running bool
	mu      sync.Mutex
}

// Init initializes the core engine singleton.
func Init(ctx context.Context) {
	once.Do(func() {
		engine = &TaskEngine{
			queue:  queue.NewTaskQueue[Executable](),
			logger: log.FromContext(ctx).WithPrefix("core"),
			quit:   make(chan struct{}),
		}
	})
}

// Run starts the worker pool in a non-blocking way.
func Run(ctx context.Context) {
	if engine == nil {
		Init(ctx)
	}

	engine.mu.Lock()
	if engine.running {
		engine.mu.Unlock()
		return
	}
	engine.running = true
	engine.mu.Unlock()

	workerCount := config.C().Workers
	if workerCount <= 0 {
		workerCount = 1
	}

	engine.logger.Infof("🚀 Starting Core Engine with %d workers", workerCount)

	for i := 0; i < workerCount; i++ {
		engine.wg.Add(1)
		go engine.workerLoop(ctx, i)
	}
}

// Stop signals all workers to stop after finishing current tasks.
func Stop() {
	if engine == nil {
		return
	}

	engine.mu.Lock()
	if !engine.running {
		engine.mu.Unlock()
		return
	}
	engine.running = false
	engine.mu.Unlock()

	engine.logger.Warn("🛑 Stopping Core Engine gracefully...")
	close(engine.quit)
	engine.queue.Close()
	engine.wg.Wait()
	engine.logger.Info("✅ Core Engine stopped.")
}

func AddTask(ctx context.Context, task Executable) error {
	if engine == nil {
		Init(ctx)
	}
	return engine.queue.Add(queue.NewTask(ctx, task.TaskID(), task.Title(), task))
}

func CancelTask(ctx context.Context, id string) error {
	if engine == nil {
		return errors.New("core engine not initialized")
	}
	return engine.queue.CancelTask(id)
}

func GetLength(ctx context.Context) int {
	if engine == nil {
		return 0
	}
	return engine.queue.ActiveLength()
}

func GetRunningTasks(ctx context.Context) []queue.TaskInfo {
	if engine == nil {
		return nil
	}
	return engine.queue.RunningTasks()
}

func GetQueuedTasks(ctx context.Context) []queue.TaskInfo {
	if engine == nil {
		return nil
	}
	return engine.queue.QueuedTasks()
}

func (e *TaskEngine) workerLoop(ctx context.Context, workerID int) {
	defer e.wg.Done()
	logger := e.logger.With("worker", workerID)

	logger.Debug("Worker started")

	for {
		select {
		case <-e.quit:
			return
		case <-ctx.Done():
			return
		default:
		}

		qtask, err := e.queue.Get()
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			logger.Errorf("Failed to get task from queue: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}

		select {
		case <-e.quit:
			e.queue.Done(qtask.ID)
			return
		case <-ctx.Done():
			e.queue.Done(qtask.ID)
			return
		default:
		}

		e.processTaskSafe(ctx, qtask, logger)
		e.queue.Done(qtask.ID)
	}
}

func (e *TaskEngine) processTaskSafe(ctx context.Context, qtask *queue.Task[Executable], logger *log.Logger) {
	exe := qtask.Data
	taskID := exe.TaskID()

	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("🔥 PANIC RECOVERED in task %s: %v\nStack: %s", taskID, r, string(debug.Stack()))
			e.runHooks(ctx, config.C().Hook.Exec.TaskFail, taskID)
		}
	}()

	logger.Infof("⚡ Processing: %s", taskID)

	e.runHooks(qtask.Context(), config.C().Hook.Exec.TaskBeforeStart, taskID)

	var finalErr error
	for attempt := 0; attempt <= MaxRetries; attempt++ {
		if isCancelled(qtask.Context()) {
			finalErr = context.Canceled
			break
		}

		err := exe.Execute(qtask.Context())
		if err == nil {
			finalErr = nil
			break
		}

		if errors.Is(err, context.Canceled) {
			finalErr = err
			break
		}

		logger.Warnf("Task %s failed (Attempt %d/%d): %v", taskID, attempt+1, MaxRetries, err)
		finalErr = err

		if attempt < MaxRetries {
			sleepTime := BaseRetryBackoff * time.Duration(1<<attempt)
			logger.Debugf("Retrying task %s in %s...", taskID, sleepTime)

			select {
			case <-time.After(sleepTime):
			case <-qtask.Context().Done():
				finalErr = context.Canceled
				goto EndLoop
			case <-e.quit:
				finalErr = context.Canceled
				goto EndLoop
			}
		}
	}

EndLoop:
	if finalErr != nil {
		if errors.Is(finalErr, context.Canceled) {
			logger.Infof("🚫 Task Canceled: %s", taskID)
			e.runHooks(ctx, config.C().Hook.Exec.TaskCancel, taskID)
		} else {
			logger.Errorf("❌ Task Failed after retries: %s | Error: %v", taskID, finalErr)
			e.runHooks(ctx, config.C().Hook.Exec.TaskFail, taskID)
		}
	} else {
		logger.Infof("✅ Task Completed: %s", taskID)
		e.runHooks(ctx, config.C().Hook.Exec.TaskSuccess, taskID)
	}
}

func (e *TaskEngine) runHooks(ctx context.Context, command string, taskID string) {
	if command == "" {
		return
	}
	if err := ExecCommandString(ctx, command); err != nil {
		e.logger.Warnf("Hook execution failed for task %s (cmd: %s): %v", taskID, command, err)
	}
}

func isCancelled(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}
