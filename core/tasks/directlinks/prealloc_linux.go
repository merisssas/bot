//go:build linux

package directlinks

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"

	"github.com/merisssas/bot/common/utils/fsutil"
)

func preallocateFile(file *fsutil.File, size int64) error {
	if size <= 0 {
		return nil
	}
	if err := unix.Fallocate(int(file.Fd()), 0, 0, size); err != nil {
		if !errors.Is(err, unix.EOPNOTSUPP) && !errors.Is(err, unix.ENOSYS) && !errors.Is(err, unix.EINVAL) {
			return fmt.Errorf("failed to fallocate file: %w", err)
		}
	}
	if err := file.Truncate(size); err != nil {
		return fmt.Errorf("failed to truncate file: %w", err)
	}
	return nil
}
