package ytdlp

import "fmt"

type ErrorCode string

const (
	ErrorCodeInvalidInput   ErrorCode = "invalid_input"
	ErrorCodeWorkspace      ErrorCode = "workspace"
	ErrorCodeDownloadFailed ErrorCode = "download_failed"
	ErrorCodeTransferFailed ErrorCode = "transfer_failed"
	ErrorCodeIntegrity      ErrorCode = "integrity_failed"
	ErrorCodeConfig         ErrorCode = "config_error"
	ErrorCodeCanceled       ErrorCode = "canceled"
)

type TaskError struct {
	Code ErrorCode
	Op   string
	Err  error
}

func (e *TaskError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("%s: %s", e.Code, e.Op)
	}
	return fmt.Sprintf("%s: %s: %v", e.Code, e.Op, e.Err)
}

func (e *TaskError) Unwrap() error {
	return e.Err
}

func (e *TaskError) ExitCode() int {
	switch e.Code {
	case ErrorCodeInvalidInput:
		return 2
	case ErrorCodeWorkspace:
		return 3
	case ErrorCodeDownloadFailed:
		return 4
	case ErrorCodeTransferFailed:
		return 5
	case ErrorCodeIntegrity:
		return 6
	case ErrorCodeConfig:
		return 7
	case ErrorCodeCanceled:
		return 130
	default:
		return 1
	}
}

func newTaskError(code ErrorCode, op string, err error) error {
	return &TaskError{
		Code: code,
		Op:   op,
		Err:  err,
	}
}
