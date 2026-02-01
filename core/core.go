package core

import (
	"context"
	"errors"
	"sync"

	"github.com/charmbracelet/log"
	"github.com/merisssas/bot/config"
	"github.com/merisssas/bot/pkg/enums/tasktype"
	"github.com/merisssas/bot/pkg/queue"
)

var (
	queueInstance *queue.TaskQueue[Executable]
	queueOnce     sync.Once
)

type Executable interface {
	Type() tasktype.TaskType
	Title() string
	TaskID() string
	Execute(ctx context.Context) error
}

func worker(ctx context.Context, qe *queue.TaskQueue[Executable]) {
	logger := log.FromContext(ctx)
	execHooks := config.C().Hook.Exec
	for {
		qtask, err := qe.Get()
		if err != nil {
			logger.Error("Failed to get task from queue:", err)
			break // queue closed and empty
		}
		func() {
			exe := qtask.Data
			logger.Infof("Processing task: %s", exe.TaskID())
			if err := ExecCommandString(qtask.Context(), execHooks.TaskBeforeStart); err != nil {
				logger.Errorf("Failed to execute before start hook for task %s: %v", exe.TaskID(), err)
			}
			if err := exe.Execute(qtask.Context()); err != nil {
				if errors.Is(err, context.Canceled) {
					logger.Infof("Task %s was canceled", exe.TaskID())
					if err := ExecCommandString(ctx, execHooks.TaskCancel); err != nil {
						logger.Errorf("Failed to execute cancel hook for task %s: %v", exe.TaskID(), err)
					}
				} else {
					logger.Errorf("Failed to execute task %s: %v", exe.TaskID(), err)
					if err := ExecCommandString(ctx, execHooks.TaskFail); err != nil {
						logger.Errorf("Failed to execute fail hook for task %s: %v", exe.TaskID(), err)
					}
				}
			} else {
				logger.Infof("Task %s completed successfully", exe.TaskID())
				if err := ExecCommandString(ctx, execHooks.TaskSuccess); err != nil {
					logger.Errorf("Failed to execute success hook for task %s: %v", exe.TaskID(), err)
				}
			}
			qe.Done(qtask.ID)
		}()
	}
}

func Run(ctx context.Context) {
	log.FromContext(ctx).Info("Start processing tasks...")
	queueOnce.Do(func() {
		queueInstance = queue.NewTaskQueue[Executable]()
	})
	for range config.C().Workers {
		go worker(ctx, queueInstance)
	}

}

func AddTask(ctx context.Context, task Executable) error {
	queueOnce.Do(func() {
		queueInstance = queue.NewTaskQueue[Executable]()
	})
	return queueInstance.Add(queue.NewTask(ctx, task.TaskID(), task.Title(), task))
}

func CancelTask(ctx context.Context, id string) error {
	queueOnce.Do(func() {
		queueInstance = queue.NewTaskQueue[Executable]()
	})
	err := queueInstance.CancelTask(id)
	return err
}

func GetLength(ctx context.Context) int {
	queueOnce.Do(func() {
		queueInstance = queue.NewTaskQueue[Executable]()
	})
	return queueInstance.ActiveLength()
}

func GetRunningTasks(ctx context.Context) []queue.TaskInfo {
	queueOnce.Do(func() {
		queueInstance = queue.NewTaskQueue[Executable]()
	})
	return queueInstance.RunningTasks()
}

func GetQueuedTasks(ctx context.Context) []queue.TaskInfo {
	queueOnce.Do(func() {
		queueInstance = queue.NewTaskQueue[Executable]()
	})
	return queueInstance.QueuedTasks()
}
