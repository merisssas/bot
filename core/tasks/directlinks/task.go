package directlinks

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/log"
	"github.com/merisssas/Bot/config"
	"github.com/merisssas/Bot/core"
	"github.com/merisssas/Bot/pkg/enums/tasktype"
	"github.com/merisssas/Bot/storage"
)

type File struct {
	Name            string
	URL             string
	Size            int64
	ContentType     string
	IsResumable     bool
	Priority        int
	StorageFileName string
	downloadedBytes atomic.Int64
}

func (f *File) FileName() string {
	if f.StorageFileName != "" {
		return f.StorageFileName
	}
	return f.Name
}

func (f *File) FileSize() int64 {
	return f.Size
}

func (f *File) DownloadedBytes() int64 {
	return f.downloadedBytes.Load()
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

	logger          *log.Logger
	logFile         *os.File
	limiter         *rateLimiter
	overwritePolicy overwritePolicy
	retryPolicy     retryPolicy
	jitter          *jitterSource
	userAgent       string
	authUsername    string
	authPassword    string
	enableResume    bool
	segmentConfig   segmentConfig
	pauseMu         sync.Mutex
	paused          bool
}

// Title implements core.Exectable.
func (t *Task) Title() string {
	fileName := "Unknown"
	if len(t.files) > 0 {
		fileName = t.files[0].StorageFileName
		if fileName == "" {
			fileName = t.files[0].Name
		}
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
		fileName := t.files[0].StorageFileName
		if fileName == "" {
			fileName = t.files[0].Name
		}
		if t.StorPath == "" {
			return fileName
		}
		return path.Join(t.StorPath, fileName)
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
	directCfg := config.C().Directlinks
	files := make([]*File, 0, len(links))
	for _, link := range links {
		files = append(files, &File{
			URL:      link,
			Priority: directCfg.DefaultPriority,
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

	task := &Task{
		ID:           id,
		ctx:          ctx,
		files:        files,
		Storage:      stor,
		StorPath:     normalizeStorPath(storPath),
		Progress:     progressTracker,
		stream:       stream,
		client:       httpClient,
		processing:   make(map[string]*File),
		processingMu: sync.RWMutex{},
		failed:       make(map[string]error),
		failedMu:     sync.RWMutex{},
		totalFiles:   int64(len(files)),
	}
	task.retryPolicy = newRetryPolicy(directCfg.MaxRetries, directCfg.RetryBaseDelay, directCfg.RetryMaxDelay)
	task.jitter = newJitterSource()
	task.segmentConfig = newSegmentConfig(directCfg.SegmentConcurrency, directCfg.MinMultipartSize, directCfg.MinSegmentSize)
	task.overwritePolicy = parseOverwritePolicy(directCfg.OverwritePolicy)
	task.userAgent = directCfg.UserAgent
	task.authUsername = directCfg.AuthUsername
	task.authPassword = directCfg.AuthPassword
	task.enableResume = directCfg.EnableResume
	task.limiter = newRateLimiter(directCfg.LimitRate, directCfg.BurstRate)
	task.logger, task.logFile = buildTaskLogger(ctx, directCfg.LogFile, directCfg.LogLevel)
	if task.logger != nil {
		task.ctx = log.WithContext(task.ctx, task.logger)
	}
	if task.shouldDisableStream() {
		task.stream = false
	}
	return task
}

func normalizeStorPath(storPath string) string {
	clean := strings.TrimSpace(storPath)
	if clean == "" {
		return ""
	}

	clean = filepath.Clean(clean)
	if clean == "." || clean == "/" || clean == "\\" {
		return ""
	}

	if volume := filepath.VolumeName(clean); volume != "" {
		clean = strings.TrimPrefix(clean, volume)
	}
	if filepath.IsAbs(clean) {
		clean = strings.TrimLeft(clean, "/\\")
	}

	if clean == "" || clean == "." {
		return ""
	}
	if strings.HasPrefix(clean, "..") || strings.Contains(clean, "/..") || strings.Contains(clean, "\\..") {
		return ""
	}

	return filepath.ToSlash(clean)
}
