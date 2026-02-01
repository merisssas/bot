//go:build !linux

package directlinks

import (
	"fmt"

	"github.com/merisssas/bot/common/utils/fsutil"
)

func preallocateFile(file *fsutil.File, size int64) error {
	if size <= 0 {
		return nil
	}
	if err := file.Truncate(size); err != nil {
		return fmt.Errorf("failed to truncate file: %w", err)
	}
	return nil
}
