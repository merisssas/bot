package config

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/duke-git/lancet/v2/slice"
	"github.com/merisssas/Bot/config/storage"
	"github.com/spf13/viper"
	"golang.org/x/net/proxy"
)

type Config struct {
	Lang         string            `toml:"lang" mapstructure:"lang" json:"lang"`
	Workers      int               `toml:"workers" mapstructure:"workers"`
	Retry        int               `toml:"retry" mapstructure:"retry"`
	NoCleanCache bool              `toml:"no_clean_cache" mapstructure:"no_clean_cache" json:"no_clean_cache"`
	Threads      int               `toml:"threads" mapstructure:"threads" json:"threads"`
	Stream       bool              `toml:"stream" mapstructure:"stream" json:"stream"`
	Proxy        string            `toml:"proxy" mapstructure:"proxy" json:"proxy"`
	Aria2        aria2Config       `toml:"aria2" mapstructure:"aria2" json:"aria2"`
	Ytdlp        ytdlpConfig       `toml:"ytdlp" mapstructure:"ytdlp" json:"ytdlp"`
	Directlinks  directlinksConfig `toml:"directlinks" mapstructure:"directlinks" json:"directlinks"`
	TFile        tfileConfig       `toml:"tfile" mapstructure:"tfile" json:"tfile"`

	Cache    cacheConfig             `toml:"cache" mapstructure:"cache" json:"cache"`
	Users    []userConfig            `toml:"users" mapstructure:"users" json:"users"`
	Temp     tempConfig              `toml:"temp" mapstructure:"temp"`
	DB       dbConfig                `toml:"db" mapstructure:"db"`
	Telegram telegramConfig          `toml:"telegram" mapstructure:"telegram"`
	Storages []storage.StorageConfig `toml:"-" mapstructure:"-" json:"storages"`
	Parser   parserConfig            `toml:"parser" mapstructure:"parser" json:"parser"`
	Hook     hookConfig              `toml:"hook" mapstructure:"hook" json:"hook"`
}

type aria2Config struct {
	Enable              bool              `toml:"enable" mapstructure:"enable" json:"enable"`
	Url                 string            `toml:"url" mapstructure:"url" json:"url"`
	Secret              string            `toml:"secret" mapstructure:"secret" json:"secret"`
	KeepFile            bool              `toml:"keep_file" mapstructure:"keep_file" json:"keep_file"`
	RemoveAfterTransfer *bool             `toml:"remove_after_transfer" mapstructure:"remove_after_transfer" json:"remove_after_transfer"`
	MaxRetries          int               `toml:"max_retries" mapstructure:"max_retries" json:"max_retries"`
	RetryBaseDelay      time.Duration     `toml:"retry_base_delay" mapstructure:"retry_base_delay" json:"retry_base_delay"`
	RetryMaxDelay       time.Duration     `toml:"retry_max_delay" mapstructure:"retry_max_delay" json:"retry_max_delay"`
	EnableResume        *bool             `toml:"enable_resume" mapstructure:"enable_resume" json:"enable_resume"`
	Split               int               `toml:"split" mapstructure:"split" json:"split"`
	MaxConnPerServer    int               `toml:"max_conn_per_server" mapstructure:"max_conn_per_server" json:"max_conn_per_server"`
	MinSplitSize        string            `toml:"min_split_size" mapstructure:"min_split_size" json:"min_split_size"`
	LimitRate           string            `toml:"limit_rate" mapstructure:"limit_rate" json:"limit_rate"`
	BurstRate           string            `toml:"burst_rate" mapstructure:"burst_rate" json:"burst_rate"`
	BurstDuration       time.Duration     `toml:"burst_duration" mapstructure:"burst_duration" json:"burst_duration"`
	OverwritePolicy     string            `toml:"overwrite_policy" mapstructure:"overwrite_policy" json:"overwrite_policy"`
	DryRun              bool              `toml:"dry_run" mapstructure:"dry_run" json:"dry_run"`
	ChecksumAlgorithm   string            `toml:"checksum_algorithm" mapstructure:"checksum_algorithm" json:"checksum_algorithm"`
	ExpectedChecksum    string            `toml:"expected_checksum" mapstructure:"expected_checksum" json:"expected_checksum"`
	UserAgent           string            `toml:"user_agent" mapstructure:"user_agent" json:"user_agent"`
	Proxy               string            `toml:"proxy" mapstructure:"proxy" json:"proxy"`
	Headers             map[string]string `toml:"headers" mapstructure:"headers" json:"headers"`
	DefaultPriority     int               `toml:"default_priority" mapstructure:"default_priority" json:"default_priority"`
}

func (c aria2Config) RemoveAfterTransferEnabled() bool {
	if c.KeepFile {
		return false
	}
	if c.RemoveAfterTransfer == nil {
		return true
	}
	return *c.RemoveAfterTransfer
}

type ytdlpConfig struct {
	MaxRetries            int           `toml:"max_retries" mapstructure:"max_retries" json:"max_retries"`
	RetryBaseDelay        time.Duration `toml:"retry_base_delay" mapstructure:"retry_base_delay" json:"retry_base_delay"`
	RetryMaxDelay         time.Duration `toml:"retry_max_delay" mapstructure:"retry_max_delay" json:"retry_max_delay"`
	RetryJitter           float64       `toml:"retry_jitter" mapstructure:"retry_jitter" json:"retry_jitter"`
	DownloadConcurrency   int           `toml:"download_concurrency" mapstructure:"download_concurrency" json:"download_concurrency"`
	FragmentConcurrency   int           `toml:"fragment_concurrency" mapstructure:"fragment_concurrency" json:"fragment_concurrency"`
	EnableResume          bool          `toml:"enable_resume" mapstructure:"enable_resume" json:"enable_resume"`
	Proxy                 string        `toml:"proxy" mapstructure:"proxy" json:"proxy"`
	ProxyPool             []string      `toml:"proxy_pool" mapstructure:"proxy_pool" json:"proxy_pool"`
	ExternalDownloader    string        `toml:"external_downloader" mapstructure:"external_downloader" json:"external_downloader"`
	ExternalDownloaderArg []string      `toml:"external_downloader_args" mapstructure:"external_downloader_args" json:"external_downloader_args"`
	LimitRate             string        `toml:"limit_rate" mapstructure:"limit_rate" json:"limit_rate"`
	ThrottledRate         string        `toml:"throttled_rate" mapstructure:"throttled_rate" json:"throttled_rate"`
	AdaptiveLimit         bool          `toml:"adaptive_limit" mapstructure:"adaptive_limit" json:"adaptive_limit"`
	AdaptiveLimitMinRate  string        `toml:"adaptive_limit_min_rate" mapstructure:"adaptive_limit_min_rate" json:"adaptive_limit_min_rate"`
	AdaptiveLimitMaxRate  string        `toml:"adaptive_limit_max_rate" mapstructure:"adaptive_limit_max_rate" json:"adaptive_limit_max_rate"`
	OverwritePolicy       string        `toml:"overwrite_policy" mapstructure:"overwrite_policy" json:"overwrite_policy"`
	FormatSort            string        `toml:"format_sort" mapstructure:"format_sort" json:"format_sort"`
	FormatFallbacks       []string      `toml:"format_fallbacks" mapstructure:"format_fallbacks" json:"format_fallbacks"`
	RecodeVideo           string        `toml:"recode_video" mapstructure:"recode_video" json:"recode_video"`
	MergeOutputFormat     string        `toml:"merge_output_format" mapstructure:"merge_output_format" json:"merge_output_format"`
	EnableFragmentRepair  bool          `toml:"enable_fragment_repair" mapstructure:"enable_fragment_repair" json:"enable_fragment_repair"`
	FragmentRepairPasses  int           `toml:"fragment_repair_passes" mapstructure:"fragment_repair_passes" json:"fragment_repair_passes"`
	DedupEnabled          bool          `toml:"dedup_enabled" mapstructure:"dedup_enabled" json:"dedup_enabled"`
	PersistState          bool          `toml:"persist_state" mapstructure:"persist_state" json:"persist_state"`
	StateDir              string        `toml:"state_dir" mapstructure:"state_dir" json:"state_dir"`
	CleanupStateOnSuccess bool          `toml:"cleanup_state_on_success" mapstructure:"cleanup_state_on_success" json:"cleanup_state_on_success"`
	RateLimitMinInterval  time.Duration `toml:"rate_limit_min_interval" mapstructure:"rate_limit_min_interval" json:"rate_limit_min_interval"`
	RateLimitMaxInterval  time.Duration `toml:"rate_limit_max_interval" mapstructure:"rate_limit_max_interval" json:"rate_limit_max_interval"`
	RateLimitJitter       float64       `toml:"rate_limit_jitter" mapstructure:"rate_limit_jitter" json:"rate_limit_jitter"`
	FingerprintRandomize  bool          `toml:"fingerprint_randomize" mapstructure:"fingerprint_randomize" json:"fingerprint_randomize"`
	UserAgent             string        `toml:"user_agent" mapstructure:"user_agent" json:"user_agent"`
	UserAgentPool         []string      `toml:"user_agent_pool" mapstructure:"user_agent_pool" json:"user_agent_pool"`
	HappyEyeballs         bool          `toml:"happy_eyeballs" mapstructure:"happy_eyeballs" json:"happy_eyeballs"`
	DryRun                bool          `toml:"dry_run" mapstructure:"dry_run" json:"dry_run"`
	ChecksumAlgorithm     string        `toml:"checksum_algorithm" mapstructure:"checksum_algorithm" json:"checksum_algorithm"`
	ExpectedChecksum      string        `toml:"expected_checksum" mapstructure:"expected_checksum" json:"expected_checksum"`
	WriteChecksumFile     bool          `toml:"write_checksum_file" mapstructure:"write_checksum_file" json:"write_checksum_file"`
	LogFile               string        `toml:"log_file" mapstructure:"log_file" json:"log_file"`
	LogLevel              string        `toml:"log_level" mapstructure:"log_level" json:"log_level"`
}

type directlinksConfig struct {
	MaxConcurrency     int           `toml:"max_concurrency" mapstructure:"max_concurrency" json:"max_concurrency"`
	SegmentConcurrency int           `toml:"segment_concurrency" mapstructure:"segment_concurrency" json:"segment_concurrency"`
	MinMultipartSize   string        `toml:"min_multipart_size" mapstructure:"min_multipart_size" json:"min_multipart_size"`
	MinSegmentSize     string        `toml:"min_segment_size" mapstructure:"min_segment_size" json:"min_segment_size"`
	EnableResume       bool          `toml:"enable_resume" mapstructure:"enable_resume" json:"enable_resume"`
	MaxRetries         int           `toml:"max_retries" mapstructure:"max_retries" json:"max_retries"`
	RetryBaseDelay     time.Duration `toml:"retry_base_delay" mapstructure:"retry_base_delay" json:"retry_base_delay"`
	RetryMaxDelay      time.Duration `toml:"retry_max_delay" mapstructure:"retry_max_delay" json:"retry_max_delay"`
	LimitRate          string        `toml:"limit_rate" mapstructure:"limit_rate" json:"limit_rate"`
	BurstRate          string        `toml:"burst_rate" mapstructure:"burst_rate" json:"burst_rate"`
	DryRun             bool          `toml:"dry_run" mapstructure:"dry_run" json:"dry_run"`
	OverwritePolicy    string        `toml:"overwrite_policy" mapstructure:"overwrite_policy" json:"overwrite_policy"`
	ChecksumAlgorithm  string        `toml:"checksum_algorithm" mapstructure:"checksum_algorithm" json:"checksum_algorithm"`
	ExpectedChecksum   string        `toml:"expected_checksum" mapstructure:"expected_checksum" json:"expected_checksum"`
	WriteChecksumFile  bool          `toml:"write_checksum_file" mapstructure:"write_checksum_file" json:"write_checksum_file"`
	LogFile            string        `toml:"log_file" mapstructure:"log_file" json:"log_file"`
	LogLevel           string        `toml:"log_level" mapstructure:"log_level" json:"log_level"`
	UserAgent          string        `toml:"user_agent" mapstructure:"user_agent" json:"user_agent"`
	Proxy              string        `toml:"proxy" mapstructure:"proxy" json:"proxy"`
	AuthUsername       string        `toml:"auth_username" mapstructure:"auth_username" json:"auth_username"`
	AuthPassword       string        `toml:"auth_password" mapstructure:"auth_password" json:"auth_password"`
	DefaultPriority    int           `toml:"default_priority" mapstructure:"default_priority" json:"default_priority"`
}

type tfileConfig struct {
	MaxRetries      int           `toml:"max_retries" mapstructure:"max_retries" json:"max_retries"`
	RetryBaseDelay  time.Duration `toml:"retry_base_delay" mapstructure:"retry_base_delay" json:"retry_base_delay"`
	RetryMaxDelay   time.Duration `toml:"retry_max_delay" mapstructure:"retry_max_delay" json:"retry_max_delay"`
	RetryJitter     float64       `toml:"retry_jitter" mapstructure:"retry_jitter" json:"retry_jitter"`
	DryRun          bool          `toml:"dry_run" mapstructure:"dry_run" json:"dry_run"`
	OverwritePolicy string        `toml:"overwrite_policy" mapstructure:"overwrite_policy" json:"overwrite_policy"`
	VerifyHashes    bool          `toml:"verify_hashes" mapstructure:"verify_hashes" json:"verify_hashes"`
}

var cfg = &Config{}

func C() Config {
	return *cfg
}

func (c Config) GetStorageByName(name string) storage.StorageConfig {
	for _, storage := range c.Storages {
		if storage.GetName() == name {
			return storage
		}
	}
	return nil
}

func Init(ctx context.Context, configFile ...string) error {
	viper.SetConfigType("toml")
	viper.SetEnvPrefix("TELELOAD")
	viper.AutomaticEnv()
	replacer := strings.NewReplacer(".", "_")
	viper.SetEnvKeyReplacer(replacer)

	// If a config file path is specified, use that file.
	// The config file can be provided via an http(s) URL.
	if len(configFile) > 0 && configFile[0] != "" {
		cfg := configFile[0]
		if strings.HasPrefix(cfg, "http://") || strings.HasPrefix(cfg, "https://") {
			// Use a remote config file.
			resp, err := http.Get(cfg)
			if err != nil {
				return fmt.Errorf("failed to fetch remote config file: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("failed to fetch remote config file: status code %d", resp.StatusCode)
			}
			if err := viper.ReadConfig(resp.Body); err != nil {
				return fmt.Errorf("failed to read remote config file: %w", err)
			}
		} else {
			viper.SetConfigFile(cfg)
		}
	} else {
		viper.SetConfigName("config")
		viper.AddConfigPath(".")
		viper.AddConfigPath("/etc/Teleload/")
	}

	defaultConfigs := map[string]any{
		// Base config
		"lang":    "en",
		"workers": 3,
		"retry":   3,
		"threads": 4,

		// Cache config
		"cache.ttl":          86400,
		"cache.num_counters": 1e5,
		"cache.max_cost":     1e6,

		// Telegram
		"telegram.app_id":          1025907,
		"telegram.app_hash":        "452b0359b988148995f22ff0f4229750",
		"telegram.rpc_retry":       5,
		"telegram.userbot.enable":  false,
		"telegram.userbot.session": "data/usersession.db",

		// Temporary directory
		"temp.base_path": "cache/",

		// Database
		"db.path":    "data/Teleload.db",
		"db.session": "data/session.db",

		// yt-dlp defaults
		"ytdlp.max_retries":              5,
		"ytdlp.retry_base_delay":         "2s",
		"ytdlp.retry_max_delay":          "30s",
		"ytdlp.retry_jitter":             0.25,
		"ytdlp.download_concurrency":     2,
		"ytdlp.fragment_concurrency":     16,
		"ytdlp.enable_resume":            true,
		"ytdlp.proxy_pool":               []string{},
		"ytdlp.limit_rate":               "",
		"ytdlp.throttled_rate":           "",
		"ytdlp.adaptive_limit":           true,
		"ytdlp.adaptive_limit_min_rate":  "512K",
		"ytdlp.adaptive_limit_max_rate":  "0",
		"ytdlp.overwrite_policy":         "rename",
		"ytdlp.format_sort":              "res:1080,vcodec:h264,acodec:aac",
		"ytdlp.format_fallbacks":         []string{"bestvideo+bestaudio/best", "best"},
		"ytdlp.recode_video":             "mp4",
		"ytdlp.merge_output_format":      "mp4",
		"ytdlp.enable_fragment_repair":   true,
		"ytdlp.fragment_repair_passes":   2,
		"ytdlp.dedup_enabled":            true,
		"ytdlp.persist_state":            true,
		"ytdlp.state_dir":                "data/ytdlp_state",
		"ytdlp.cleanup_state_on_success": true,
		"ytdlp.rate_limit_min_interval":  "0ms",
		"ytdlp.rate_limit_max_interval":  "5s",
		"ytdlp.rate_limit_jitter":        0.2,
		"ytdlp.fingerprint_randomize":    true,
		"ytdlp.user_agent_pool": []string{
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
			"Mozilla/5.0 (Macintosh; Intel Mac OS X 13_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Safari/605.1.15",
			"Mozilla/5.0 (X11; Linux x86_64) Gecko/20100101 Firefox/122.0",
		},
		"ytdlp.happy_eyeballs": true,
		"ytdlp.dry_run":        false,
		"ytdlp.log_level":      "info",
		"ytdlp.user_agent":     "Mozilla/5.0 (Windows NT 10.0; Win64; x64) UltimateDownloader/2.0",

		// Directlinks defaults
		"directlinks.max_concurrency":     4,
		"directlinks.segment_concurrency": 16,
		"directlinks.min_multipart_size":  "5MB",
		"directlinks.min_segment_size":    "1MB",
		"directlinks.enable_resume":       true,
		"directlinks.max_retries":         5,
		"directlinks.retry_base_delay":    "500ms",
		"directlinks.retry_max_delay":     "10s",
		"directlinks.limit_rate":          "",
		"directlinks.burst_rate":          "2MB",
		"directlinks.dry_run":             false,
		"directlinks.overwrite_policy":    "rename",
		"directlinks.checksum_algorithm":  "",
		"directlinks.expected_checksum":   "",
		"directlinks.write_checksum_file": false,
		"directlinks.log_file":            "",
		"directlinks.log_level":           "info",
		"directlinks.user_agent":          "Mozilla/5.0 (Windows NT 10.0; Win64; x64) UltimateDownloader/2.0",
		"directlinks.proxy":               "",
		"directlinks.auth_username":       "",
		"directlinks.auth_password":       "",
		"directlinks.default_priority":    0,

		// tfile (Telegram file) defaults
		"tfile.max_retries":      3,
		"tfile.retry_base_delay": "500ms",
		"tfile.retry_max_delay":  "10s",
		"tfile.retry_jitter":     0.25,
		"tfile.dry_run":          false,
		"tfile.overwrite_policy": "rename",
		"tfile.verify_hashes":    true,
	}

	for key, value := range defaultConfigs {
		viper.SetDefault(key, value)
	}

	if err := viper.SafeWriteConfigAs("config.toml"); err != nil {
		if _, ok := err.(viper.ConfigFileAlreadyExistsError); !ok {
			return fmt.Errorf("error saving default config: %w", err)
		}
	}

	if err := viper.ReadInConfig(); err != nil {
		fmt.Println("Error reading config file, ", err)
		return err
	}

	if err := viper.Unmarshal(cfg); err != nil {
		fmt.Println("Error unmarshalling config file, ", err)
		return err
	}

	storagesConfig, err := storage.LoadStorageConfigs(viper.GetViper())
	if err != nil {
		return fmt.Errorf("error loading storage configs: %w", err)
	}
	cfg.Storages = storagesConfig

	storageNames := make(map[string]struct{})
	for _, storage := range cfg.Storages {
		if _, ok := storageNames[storage.GetName()]; ok {
			return fmt.Errorf("duplicate storage name: %s", storage.GetName())
		}
		storageNames[storage.GetName()] = struct{}{}
	}

	if cfg.Workers < 1 {
		cfg.Workers = 1
	}
	if cfg.Threads < 1 {
		cfg.Threads = 1
	}
	if cfg.Retry < 1 {
		cfg.Retry = 1
	}
	if cfg.TFile.MaxRetries < 1 {
		cfg.TFile.MaxRetries = cfg.Retry
	}
	if cfg.TFile.RetryBaseDelay <= 0 {
		cfg.TFile.RetryBaseDelay = 500 * time.Millisecond
	}
	if cfg.TFile.RetryMaxDelay <= 0 {
		cfg.TFile.RetryMaxDelay = 10 * time.Second
	}
	if cfg.TFile.RetryJitter < 0 {
		cfg.TFile.RetryJitter = 0
	}

	for _, storage := range cfg.Storages {
		storages = append(storages, storage.GetName())
	}
	for _, user := range cfg.Users {
		userIDs = append(userIDs, user.ID)
		if user.Blacklist {
			userStorages[user.ID] = slice.Compact(slice.Difference(storages, user.Storages))
		} else {
			userStorages[user.ID] = user.Storages
		}
	}
	if cfg.Proxy != "" {
		http.DefaultTransport, err = newProxyTransport(cfg.Proxy)
		if err != nil {
			return fmt.Errorf("failed to create proxy transport: %w", err)
		}
	}
	return nil
}

func newProxyTransport(proxyStr string) (*http.Transport, error) {
	proxyURL, err := url.Parse(proxyStr)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	switch proxyURL.Scheme {
	case "http", "https":
		transport.Proxy = http.ProxyURL(proxyURL)

	case "socks5", "socks5h":
		dialer, err := proxy.FromURL(proxyURL, proxy.Direct)
		if err != nil {
			return nil, err
		}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.(proxy.ContextDialer).DialContext(ctx, network, addr)
		}

	default:
		return nil, fmt.Errorf("unsupported proxy type: %s", proxyURL.Scheme)
	}

	return transport, nil
}
