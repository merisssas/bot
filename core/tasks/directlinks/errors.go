package directlinks

import "fmt"

type ErrorKind string

const (
	ErrKindValidation  ErrorKind = "validation"
	ErrKindNetwork     ErrorKind = "network"
	ErrKindFilesystem  ErrorKind = "filesystem"
	ErrKindHTTP        ErrorKind = "http"
	ErrKindChecksum    ErrorKind = "checksum"
	ErrKindStorage     ErrorKind = "storage"
	ErrKindUnsupported ErrorKind = "unsupported"
	ErrKindCancelled   ErrorKind = "cancelled"
	ErrKindRateLimited ErrorKind = "rate_limited"
	ErrKindUnknown     ErrorKind = "unknown"
)

type DownloadError struct {
	Kind ErrorKind
	Err  error
	Msg  string
}

func (e DownloadError) Error() string {
	if e.Msg != "" {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Msg, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Kind, e.Err)
}

func (e DownloadError) Unwrap() error {
	return e.Err
}

func wrapError(kind ErrorKind, msg string, err error) error {
	if err == nil {
		return nil
	}
	return DownloadError{Kind: kind, Msg: msg, Err: err}
}
