package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/charmbracelet/log"
	"github.com/duke-git/lancet/v2/fileutil"
	config "github.com/merisssas/Bot/config/storage"
	storenum "github.com/merisssas/Bot/pkg/enums/storage"
	"github.com/merisssas/Bot/pkg/storagetypes"
)

const bufferSize = 32 * 1024

type Local struct {
	config config.LocalStorageConfig
	logger *log.Logger
	mu     sync.RWMutex
}

func (l *Local) Init(ctx context.Context, cfg config.StorageConfig) error {
	localConfig, ok := cfg.(*config.LocalStorageConfig)
	if !ok {
		return fmt.Errorf("failed to cast local config")
	}
	if err := localConfig.Validate(); err != nil {
		return err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.config = *localConfig
	if err := os.MkdirAll(localConfig.BasePath, 0755); err != nil {
		return fmt.Errorf("failed to create local storage directory: %w", err)
	}
	l.logger = log.FromContext(ctx).WithPrefix(fmt.Sprintf("local[%s]", l.config.Name))
	return nil
}

func (l *Local) Type() storenum.StorageType {
	return storenum.Local
}

func (l *Local) Name() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.config.Name
}

func (l *Local) JoinStoragePath(path string) string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return filepath.Join(l.config.BasePath, path)
}

func (l *Local) Save(ctx context.Context, r io.Reader, storagePath string) error {
	storagePath = l.JoinStoragePath(storagePath)

	finalPath, err := l.resolveUniquePath(ctx, storagePath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(finalPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	partPath := finalPath + ".part"
	l.logger.Infof("Saving file to %s", partPath)

	file, err := os.OpenFile(partPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open partial file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	buf := make([]byte, bufferSize)
	written, err := io.CopyBuffer(file, r, buf)
	if err != nil {
		return fmt.Errorf("write interrupted: %w", err)
	}

	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync data to disk: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close file: %w", err)
	}

	if err := os.Rename(partPath, finalPath); err != nil {
		return fmt.Errorf("failed to finalize file (rename): %w", err)
	}

	l.logger.Infof("Save complete: %s (size: %d bytes)", finalPath, written)
	return nil
}

func (l *Local) Exists(ctx context.Context, storagePath string) bool {
	absPath, err := filepath.Abs(l.JoinStoragePath(storagePath))
	if err != nil {
		return false
	}
	return fileutil.IsExist(absPath)
}

// ListFiles implements StorageListable interface
func (l *Local) ListFiles(ctx context.Context, dirPath string) ([]storagetypes.FileInfo, error) {
	absPath := l.JoinStoragePath(dirPath)

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", absPath, err)
	}

	files := make([]storagetypes.FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			l.logger.Warnf("Failed to get file info for %s: %v", entry.Name(), err)
			continue
		}

		filePath := filepath.Join(dirPath, entry.Name())
		files = append(files, storagetypes.FileInfo{
			Name:    entry.Name(),
			Path:    filePath,
			Size:    info.Size(),
			IsDir:   entry.IsDir(),
			ModTime: info.ModTime(),
		})
	}

	return files, nil
}

// OpenFile implements StorageReadable interface
func (l *Local) OpenFile(ctx context.Context, filePath string) (io.ReadCloser, int64, error) {
	absPath := l.JoinStoragePath(filePath)

	file, err := os.Open(absPath)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open file %s: %w", absPath, err)
	}

	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, 0, fmt.Errorf("failed to stat file %s: %w", absPath, err)
	}

	return file, stat.Size(), nil
}

// SaveChunk writes a file chunk at a specific offset to support parallel downloads.
func (l *Local) SaveChunk(ctx context.Context, r io.Reader, storagePath string, offset int64) error {
	absPath := l.JoinStoragePath(storagePath)

	file, err := os.OpenFile(absPath, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file for chunk write: %w", err)
	}
	defer file.Close()

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek to offset %d: %w", offset, err)
	}

	if _, err := io.Copy(file, r); err != nil {
		return fmt.Errorf("failed to write chunk: %w", err)
	}

	return nil
}

// PreAllocate reserves space for a file to reduce fragmentation.
func (l *Local) PreAllocate(storagePath string, size int64) error {
	absPath := l.JoinStoragePath(storagePath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory for preallocation: %w", err)
	}

	file, err := os.OpenFile(absPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("failed to create file for preallocation: %w", err)
	}
	defer file.Close()

	if err := file.Truncate(size); err != nil {
		return fmt.Errorf("failed to preallocate %d bytes: %w", size, err)
	}

	return nil
}

func (l *Local) resolveUniquePath(ctx context.Context, candidate string) (string, error) {
	ext := filepath.Ext(candidate)
	base := strings.TrimSuffix(candidate, ext)

	if !fileutil.IsExist(candidate) {
		return candidate, nil
	}

	for i := 1; i < 10000; i++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		newPath := fmt.Sprintf("%s_%d%s", base, i, ext)
		if !fileutil.IsExist(newPath) {
			return newPath, nil
		}
	}

	return "", fmt.Errorf("too many duplicate files, giving up")
}
