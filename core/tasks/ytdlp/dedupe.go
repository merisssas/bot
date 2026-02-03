package ytdlp

import (
	"fmt"
	"io"
	"os"
	"sync"

	"golang.org/x/crypto/blake2b"
)

type deduper struct {
	mu   sync.Mutex
	seen map[string]string
}

func newDeduper(enabled bool) *deduper {
	if !enabled {
		return nil
	}
	return &deduper{seen: make(map[string]string)}
}

func (d *deduper) IsDuplicate(path string) (bool, string, error) {
	if d == nil {
		return false, "", nil
	}
	sum, err := hashFile(path)
	if err != nil {
		return false, "", err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if prev, ok := d.seen[sum]; ok {
		return true, prev, nil
	}
	d.seen[sum] = path
	return false, "", nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher, err := blake2b.New256(nil)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}
