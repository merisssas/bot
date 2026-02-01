package directlinks

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/log"

	"github.com/merisssas/bot/config"
	"github.com/merisssas/bot/core"
	"github.com/merisssas/bot/pkg/enums/tasktype"
	"github.com/merisssas/bot/storage"
)

type File struct {
	Name string
	URL  string
	Size int64

	Headers         http.Header
	SupportsRange   bool
	DisableParallel bool
	ContentMD5      string
	ETag            string
	LastModified    string
}

func (f *File) FileName() string {
	return f.Name
}

func (f *File) FileSize() int64 {
	return f.Size
}

var _ core.Executable = (*Task)(nil)

var (
	directLinksClientOnce sync.Once
	directLinksClient     *http.Client
)

type Task struct {
	ID       string
	files    []*File
	rawLinks []string
	Storage  storage.Storage
	StorPath string
	Progress ProgressTracker

	client          *http.Client // [TODO] parallel download
	stream          bool
	totalBytes      int64            // total bytes to download
	downloadedBytes atomic.Int64     // downloaded bytes
	totalFiles      int64            // total files to download
	downloaded      atomic.Int64     // downloaded files count
	processing      map[string]*File // {"url": File}
	processingMu    sync.RWMutex
	failed          map[string]error // [TODO] errors for each file

	persisted *persistedTask
}

// Title implements core.Exectable.
func (t *Task) Title() string {
	name := "batch-download"
	if len(t.files) > 0 {
		if t.files[0].Name != "" {
			name = t.files[0].Name
		} else {
			name = "url-collection"
		}
	}
	return fmt.Sprintf("[%s] %s (-> %s)", t.Type(), name, t.Storage.Name())
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
	rawLinks := make([]string, 0, len(links))
	for _, link := range links {
		parsedURL, headers, err := parseDirectLinkInput(link)
		if err != nil {
			log.FromContext(ctx).Warnf("Skipping invalid link %q: %v", link, err)
			continue
		}
		files = append(files, &File{
			URL:     parsedURL,
			Headers: headers,
		})
		rawLinks = append(rawLinks, strings.TrimSpace(link))
	}
	task := &Task{
		ID:           id,
		files:        files,
		rawLinks:     rawLinks,
		Storage:      stor,
		StorPath:     storPath,
		Progress:     progressTracker,
		stream:       stream,
		client:       directLinksHTTPClient(),
		processing:   make(map[string]*File),
		processingMu: sync.RWMutex{},
		failed:       make(map[string]error),
		totalFiles:   int64(len(files)),
	}
	task.persisted = newPersistedTask(id, rawLinks, stor.Name(), storPath, progressTracker)
	if err := task.persistState(ctx); err != nil {
		log.FromContext(ctx).Warnf("State persistence warning for %s: %v", id, err)
	}
	return task
}

func directLinksHTTPClient() *http.Client {
	directLinksClientOnce.Do(func() {
		directLinksClient = newDirectLinksClient()
	})
	return directLinksClient
}

func newDirectLinksClient() *http.Client {
	maxConns := config.C().Workers * 2
	if maxConns < 10 {
		maxConns = 10
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 60 * time.Second,
			DualStack: true,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   maxConns,
		MaxConnsPerHost:       maxConns,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    false,
		WriteBufferSize:       64 * 1024,
		ReadBufferSize:        64 * 1024,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: config.C().InsecureSkipVerify,
		},
	}

	return &http.Client{
		Transport: transport,
	}
}
