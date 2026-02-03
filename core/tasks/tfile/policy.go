package tfile

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/merisssas/Bot/storage"
)

func resolveStoragePath(ctx context.Context, stor storage.Storage, storagePath, policy string) (string, bool, error) {
	normalized := strings.ToLower(strings.TrimSpace(policy))
	if normalized == "" {
		normalized = "rename"
	}
	if !stor.Exists(ctx, storagePath) {
		return storagePath, false, nil
	}
	switch normalized {
	case "overwrite":
		return storagePath, false, nil
	case "skip":
		return storagePath, true, nil
	case "rename":
		for i := 1; i <= 10000; i++ {
			candidate := appendSuffix(storagePath, i)
			if !stor.Exists(ctx, candidate) {
				return candidate, false, nil
			}
		}
		return "", false, fmt.Errorf("unable to find available name for %s", storagePath)
	default:
		return "", false, fmt.Errorf("unsupported overwrite policy: %s", policy)
	}
}

func appendSuffix(storagePath string, index int) string {
	dir := path.Dir(storagePath)
	base := path.Base(storagePath)
	ext := path.Ext(base)
	name := strings.TrimSuffix(base, ext)
	suffixed := fmt.Sprintf("%s (%d)%s", name, index, ext)
	if dir == "." {
		return suffixed
	}
	return path.Join(dir, suffixed)
}
