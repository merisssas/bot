package cache

import (
	"fmt"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/dgraph-io/ristretto/v2"
	"github.com/merisssas/bot/config"
)

var (
	cache     *ristretto.Cache[string, any]
	cacheOnce sync.Once
	cacheErr  error
	cacheMu   sync.RWMutex
)

func Init() error {
	cacheOnce.Do(func() {
		c, err := ristretto.NewCache(&ristretto.Config[string, any]{
			NumCounters: config.C().Cache.NumCounters,
			MaxCost:     config.C().Cache.MaxCost,
			BufferItems: 64,
			OnReject: func(item *ristretto.Item[any]) {
				log.Warnf("Cache item rejected: key=%d, value=%v", item.Key, item.Value)
			},
		})
		if err != nil {
			cacheErr = fmt.Errorf("failed to create ristretto cache: %w", err)
			return
		}
		cacheMu.Lock()
		cache = c
		cacheMu.Unlock()
	})
	if cacheErr != nil {
		return cacheErr
	}
	return nil
}

func Set(key string, value any) error {
	cacheMu.RLock()
	c := cache
	cacheMu.RUnlock()
	if c == nil {
		return fmt.Errorf("cache not initialized")
	}
	ok := c.SetWithTTL(key, value, 0, time.Duration(config.C().Cache.TTL)*time.Second)
	if !ok {
		return fmt.Errorf("failed to set value in cache")
	}
	return nil
}

func Get[T any](key string) (T, bool) {
	cacheMu.RLock()
	c := cache
	cacheMu.RUnlock()
	if c == nil {
		var zero T
		return zero, false
	}
	v, ok := c.Get(key)
	if !ok {
		var zero T
		return zero, false
	}
	vT, ok := v.(T)
	if !ok {
		var zero T
		return zero, false
	}
	return vT, true
}
