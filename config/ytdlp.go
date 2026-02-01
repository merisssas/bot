package config

type ytdlpConfig struct {
	MaxConcurrentDownloads int `toml:"max_concurrent_downloads" mapstructure:"max_concurrent_downloads" json:"max_concurrent_downloads"`
}
