package queue_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/merisssas/bot/pkg/queue"
)

// helper to create a simple Task with integer payload
func newTask(id string) *queue.Task[int] {
	return queue.NewTask(context.Background(), id, "testing", 0)
}

func TestAddAndLength(t *testing.T) {
	q := queue.NewTaskQueue[int]()
	if q.Length() != 0 {
		t.Fatalf("expected length 0, got %d", q.Length())
	}
	t1 := newTask("t1")
	if err := q.Add(t1); err != nil {
		t.Fatalf("unexpected error on Add: %v", err)
	}
	if q.Length() != 1 {
		t.Fatalf("expected length 1, got %d", q.Length())
	}
}

func TestDuplicateAdd(t *testing.T) {
	q := queue.NewTaskQueue[int]()
	t1 := newTask("dup")
	if err := q.Add(t1); err != nil {
		t.Fatalf("unexpected error on first Add: %v", err)
	}
	if err := q.Add(t1); err == nil {
		t.Fatal("expected error on duplicate Add, got nil")
	}
}

func TestCancelAndActiveLength(t *testing.T) {
	q := queue.NewTaskQueue[int]()
	t1 := newTask("1")
	t2 := newTask("2")
	q.Add(t1)
	q.Add(t2)
	// Cancel t1
	if err := q.CancelTask("1"); err != nil {
		t.Fatalf("unexpected error on CancelTask: %v", err)
	}
	// Length counts all entries
	if q.Length() != 2 {
		t.Fatalf("expected total length 2, got %d", q.Length())
	}
	// ActiveLength skips cancelled
	if got := q.ActiveLength(); got != 1 {
		t.Fatalf("expected active length 1, got %d", got)
	}
}

func TestCloseBehavior(t *testing.T) {
	q := queue.NewTaskQueue[int]()
	done := make(chan struct{})
	// consumer
	go func() {
		_, err := q.Get()
		if err == nil {
			t.Errorf("expected error when getting from closed empty queue, got nil")
		}
		close(done)
	}()
	// allow goroutine to block

	// close queue
	q.Close()
	<-done
}

func TestGetSkipsCancelledTasks(t *testing.T) {
	q := queue.NewTaskQueue[int]()
	t1 := newTask("1")
	t2 := newTask("2")
	if err := q.Add(t1); err != nil {
		t.Fatalf("unexpected error on Add: %v", err)
	}
	if err := q.Add(t2); err != nil {
		t.Fatalf("unexpected error on Add: %v", err)
	}
	if err := q.CancelTask("1"); err != nil {
		t.Fatalf("unexpected error on CancelTask: %v", err)
	}
	if err := q.CancelTask("2"); err != nil {
		t.Fatalf("unexpected error on CancelTask: %v", err)
	}

	taskCh := make(chan *queue.Task[int], 1)
	errCh := make(chan error, 1)

	go func() {
		task, err := q.Get()
		if err != nil {
			errCh <- err
			return
		}
		taskCh <- task
	}()

	select {
	case task := <-taskCh:
		t.Fatalf("expected Get to wait for a new task, got %s", task.ID)
	case err := <-errCh:
		t.Fatalf("unexpected error before new task: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	t3 := newTask("3")
	if err := q.Add(t3); err != nil {
		t.Fatalf("unexpected error on Add: %v", err)
	}

	select {
	case task := <-taskCh:
		if task.ID != "3" {
			t.Fatalf("expected task 3, got %s", task.ID)
		}
	case err := <-errCh:
		t.Fatalf("unexpected error after new task: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for task")
	}
}

func TestConcurrencySafety(t *testing.T) {
	q := queue.NewTaskQueue[int]()
	var wg sync.WaitGroup
	n := 1000
	// producers
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range n {
			q.Add(newTask(fmt.Sprintf("p%d", i)))
		}
	}()
	// consumers
	wg.Add(1)
	go func() {
		defer wg.Done()
		count := 0
		for count < n {
			_, err := q.Get()
			if err != nil {
				continue
			}
			count++
		}
	}()
	wg.Wait()
}
