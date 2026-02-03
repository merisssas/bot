package cache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/log"
	"github.com/dgraph-io/ristretto/v2"
	"golang.org/x/sync/singleflight"
)

// CacheEngine defines the cache contract to allow backend swaps.
type CacheEngine interface {
	Set(key string, value any, ttl time.Duration) bool
	Get(key string) (any, bool)
	Delete(key string)
	GetOrSet(key string, ttl time.Duration, fetch func() (any, error)) (any, error)
	Close()
}

// Config holds runtime configuration for the cache engine.
type Config struct {
	NumCounters int64
	MaxCost     int64
	BufferItems int64
	DefaultTTL  time.Duration
}

// RistrettoCache is a high-performance in-memory cache.
type RistrettoCache struct {
	store      *ristretto.Cache[string, any]
	group      singleflight.Group
	defaultTTL time.Duration
}

var (
	instance CacheEngine
	logger   *log.Logger
)

// Init initializes the cache singleton.
func Init(cfg Config, baseLogger *log.Logger) error {
	if instance != nil {
		return errors.New("cache already initialized")
	}
	if baseLogger == nil {
		baseLogger = log.NewWithOptions(os.Stdout, log.Options{Level: log.InfoLevel})
	}
	logger = baseLogger.WithPrefix("cache")

	if cfg.BufferItems == 0 {
		cfg.BufferItems = 64
	}
	if cfg.DefaultTTL < 0 {
		return fmt.Errorf("default TTL must be non-negative")
	}

	c, err := ristretto.NewCache(&ristretto.Config[string, any]{
		NumCounters: cfg.NumCounters,
		MaxCost:     cfg.MaxCost,
		BufferItems: cfg.BufferItems,
		OnReject: func(item *ristretto.Item[any]) {
			logger.Debugf("Cache item rejected: key=%v", item.Key)
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create ristretto cache: %w", err)
	}

	instance = &RistrettoCache{
		store:      c,
		defaultTTL: cfg.DefaultTTL,
	}
	logger.Info("High-performance cache engine initialized")
	return nil
}

// C returns the cache singleton.
func C() CacheEngine {
	if instance == nil {
		_ = Init(Config{
			NumCounters: 1e7,
			MaxCost:     1 << 30,
			BufferItems: 64,
			DefaultTTL:  0,
		}, logger)
		if logger != nil {
			logger.Warn("Cache accessed before Init; using defaults")
		}
	}
	return instance
}

// Set stores a value with TTL.
func (r *RistrettoCache) Set(key string, value any, ttl time.Duration) bool {
	cost := int64(1)
	switch v := value.(type) {
	case []byte:
		cost = int64(len(v))
	case string:
		cost = int64(len(v))
	}
	if cost < 1 {
		cost = 1
	}
	return r.store.SetWithTTL(key, value, cost, ttl)
}

// Get retrieves a value from cache.
func (r *RistrettoCache) Get(key string) (any, bool) {
	return r.store.Get(key)
}

// Delete removes a key from cache.
func (r *RistrettoCache) Delete(key string) {
	r.store.Del(key)
}

// Close releases cache resources.
func (r *RistrettoCache) Close() {
	r.store.Close()
}

// DefaultTTL returns the configured default TTL.
func (r *RistrettoCache) DefaultTTL() time.Duration {
	return r.defaultTTL
}

// GetOrSet returns cached value or fetches it once via singleflight.
func (r *RistrettoCache) GetOrSet(key string, ttl time.Duration, fetch func() (any, error)) (any, error) {
	if val, found := r.Get(key); found {
		return val, nil
	}

	val, err, _ := r.group.Do(key, func() (any, error) {
		if v, found := r.Get(key); found {
			return v, nil
		}
		data, err := fetch()
		if err != nil {
			return nil, err
		}
		r.Set(key, data, ttl)
		return data, nil
	})
	if err != nil {
		return nil, err
	}
	return val, nil
}

// Get returns a typed value from cache.
func Get[T any](key string) (T, bool) {
	val, found := C().Get(key)
	if !found {
		var zero T
		return zero, false
	}
	vT, ok := val.(T)
	if !ok {
		var zero T
		return zero, false
	}
	return vT, true
}

// Set stores a value using the default TTL.
func Set(key string, value any) bool {
	return C().Set(key, value, defaultTTL())
}

// SetWithTTL stores a value with a custom TTL.
func SetWithTTL(key string, value any, ttl time.Duration) bool {
	return C().Set(key, value, ttl)
}

// Fetch gets or sets a typed value using singleflight.
func Fetch[T any](ctx context.Context, key string, ttl time.Duration, fetcher func() (T, error)) (T, error) {
	_ = ctx
	val, err := C().GetOrSet(key, ttl, func() (any, error) {
		return fetcher()
	})
	if err != nil {
		var zero T
		return zero, err
	}
	vT, ok := val.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("cached value has unexpected type")
	}
	return vT, nil
}

func defaultTTL() time.Duration {
	if engine, ok := C().(interface{ DefaultTTL() time.Duration }); ok {
		return engine.DefaultTTL()
	}
	return 0
}
