package directlinks

import (
	"context"
	"errors"
	"time"
)

type unrecoverableError struct {
	err error
}

func (u unrecoverableError) Error() string {
	return u.err.Error()
}

func (u unrecoverableError) Unwrap() error {
	return u.err
}

func unrecoverable(err error) error {
	if err == nil {
		return nil
	}
	return unrecoverableError{err: err}
}

func retryWithPolicy(ctx context.Context, retries uint, delay time.Duration, fn func() error) error {
	if retries == 0 {
		retries = 1
	}

	var lastErr error
	for attempt := uint(0); attempt < retries; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		err := fn()
		if err == nil {
			return nil
		}

		var fatal unrecoverableError
		if errors.As(err, &fatal) {
			return fatal.err
		}

		lastErr = err

		if attempt+1 >= retries || delay <= 0 {
			continue
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}

	return lastErr
}
