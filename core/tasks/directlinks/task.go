package directlinks

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/merisssas/Bot/config"
	"github.com/merisssas/Bot/core"
	"github.com/merisssas/Bot/pkg/enums/tasktype"
	"github.com/merisssas/Bot/storage"
)

type File struct {
	Name        string
	URL         string
	Size        int64
	ContentType string
	IsResumable bool
}

func (f *File) FileName() string {
	return f.Name
}

func (f *File) FileSize() int64 {
	return f.Size
}

var _ core.Executable = (*Task)(nil)

type Task struct {
	ID       string
	ctx      context.Context
	files    []*File
	Storage  storage.Storage
	StorPath string
	Progress ProgressTracker

	client          *http.Client
	stream          bool
	totalBytes      int64            // total bytes to download
	downloadedBytes atomic.Int64     // downloaded bytes
	totalFiles      int64            // total files to download
	downloaded      atomic.Int64     // downloaded files count
	processing      map[string]*File // {"url": File}
	processingMu    sync.RWMutex
	failed          map[string]error // [TODO] errors for each file
	failedMu        sync.RWMutex
}

// Title implements core.Exectable.
func (t *Task) Title() string {
	fileName := "Unknown"
	if len(t.files) > 0 {
		fileName = t.files[0].Name
	}
	return fmt.Sprintf("[%s](%s...->%s:%s)", t.Type(), fileName, t.Storage.Name(), t.StorPath)
}

// DownloadedBytes implements TaskInfo.
func (t *Task) DownloadedBytes() int64 {
	return t.downloadedBytes.Load()
}

// Processing implements TaskInfo.
func (t *Task) Processing() []FileInfo {
	t.processingMu.RLock()
	defer t.processingMu.RUnlock()
	infos := make([]FileInfo, 0, len(t.processing))
	for _, f := range t.processing {
		infos = append(infos, f)
	}
	return infos
}

// StorageName implements TaskInfo.
func (t *Task) StorageName() string {
	return t.Storage.Name()
}

// StoragePath implements TaskInfo.
func (t *Task) StoragePath() string {
	if len(t.files) == 1 {
		return t.StorPath + "/" + t.files[0].Name
	}
	return t.StorPath
}

// TotalBytes implements TaskInfo.
func (t *Task) TotalBytes() int64 {
	return t.totalBytes
}

// TotalFiles implements TaskInfo.
func (t *Task) TotalFiles() int {
	return int(t.totalFiles)
}

func (t *Task) Type() tasktype.TaskType {
	return tasktype.TaskTypeDirectlinks
}

func (t *Task) TaskID() string {
	return t.ID
}

func (t *Task) RecordFailure(url string, err error) {
	t.failedMu.Lock()
	defer t.failedMu.Unlock()
	t.failed[url] = err
}

func NewTask(
	id string,
	ctx context.Context,
	links []string,
	stor storage.Storage,
	storPath string,
	progressTracker ProgressTracker,
) *Task {
	_, ok := stor.(storage.StorageCannotStream)
	stream := config.C().Stream && !ok
	files := make([]*File, 0, len(links))
	for _, link := range links {
		files = append(files, &File{
			URL: link,
		})
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          1000,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    true,
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   0,
	}

	return &Task{
		ID:           id,
		ctx:          ctx,
		files:        files,
		Storage:      stor,
		StorPath:     storPath,
		Progress:     progressTracker,
		stream:       stream,
		client:       httpClient,
		processing:   make(map[string]*File),
		processingMu: sync.RWMutex{},
		failed:       make(map[string]error),
		failedMu:     sync.RWMutex{},
		totalFiles:   int64(len(files)),
	}
}
