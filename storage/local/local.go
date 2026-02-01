package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/duke-git/lancet/v2/fileutil"
	config "github.com/merisssas/bot/config/storage"
	storenum "github.com/merisssas/bot/pkg/enums/storage"
	"github.com/merisssas/bot/pkg/storagetypes"
)

type Local struct {
	basePathAbs string
	config      config.LocalStorageConfig
	logger      *log.Logger
}

func (l *Local) Init(ctx context.Context, cfg config.StorageConfig) error {
	localConfig, ok := cfg.(*config.LocalStorageConfig)
	if !ok {
		return fmt.Errorf("failed to cast local config")
	}
	if err := localConfig.Validate(); err != nil {
		return err
	}
	basePathAbs, err := filepath.Abs(localConfig.BasePath)
	if err != nil {
		return fmt.Errorf("failed to resolve local storage base path: %w", err)
	}
	l.config = *localConfig
	l.basePathAbs = basePathAbs
	err = os.MkdirAll(basePathAbs, os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to create local storage directory: %w", err)
	}
	l.logger = log.FromContext(ctx).WithPrefix(fmt.Sprintf("local[%s]", l.config.Name))
	return nil
}

func (l *Local) Type() storenum.StorageType {
	return storenum.Local
}

func (l *Local) Name() string {
	return l.config.Name
}

func (l *Local) JoinStoragePath(path string) string {
	return filepath.Join(l.config.BasePath, path)
}

func (l *Local) resolveStoragePath(storagePath string) (string, error) {
	if l.basePathAbs == "" {
		return "", fmt.Errorf("local storage base path is not initialized")
	}
	cleanedPath := filepath.Clean(storagePath)
	var absPath string
	if filepath.IsAbs(cleanedPath) {
		absPath = cleanedPath
	} else {
		absPath = filepath.Join(l.basePathAbs, cleanedPath)
	}
	absPath, err := filepath.Abs(absPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path %s: %w", storagePath, err)
	}
	relPath, err := filepath.Rel(l.basePathAbs, absPath)
	if err != nil {
		return "", fmt.Errorf("failed to check path %s: %w", storagePath, err)
	}
	if relPath == ".." || strings.HasPrefix(relPath, fmt.Sprintf("..%c", os.PathSeparator)) {
		return "", fmt.Errorf("path %s escapes base directory", storagePath)
	}
	return absPath, nil
}

func (l *Local) Save(ctx context.Context, r io.Reader, storagePath string) error {
	l.logger.Infof("Saving file to %s", storagePath)
	storagePath, err := l.resolveStoragePath(storagePath)
	if err != nil {
		return err
	}

	ext := filepath.Ext(storagePath)
	base := strings.TrimSuffix(storagePath, ext)
	candidate := storagePath
	for i := 1; l.Exists(ctx, candidate); i++ {
		candidate = fmt.Sprintf("%s_%d%s", base, i, ext)
	}

	if err := fileutil.CreateDir(filepath.Dir(candidate)); err != nil {
		return err
	}
	file, err := os.Create(candidate)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, r)
	return err
}

func (l *Local) Exists(ctx context.Context, storagePath string) bool {
	absPath, err := l.resolveStoragePath(storagePath)
	if err != nil {
		return false
	}
	return fileutil.IsExist(absPath)
}

// ListFiles implements StorageListable interface
func (l *Local) ListFiles(ctx context.Context, dirPath string) ([]storagetypes.FileInfo, error) {
	absPath, err := l.resolveStoragePath(dirPath)
	if err != nil {
		return nil, err
	}

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
	absPath, err := l.resolveStoragePath(filePath)
	if err != nil {
		return nil, 0, err
	}

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
