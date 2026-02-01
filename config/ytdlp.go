package config

type ytdlpConfig struct {
	MaxConcurrentDownloads int `toml:"max_concurrent_downloads" mapstructure:"max_concurrent_downloads" json:"max_concurrent_downloads"`
	Proxy                  YtdlpProxyConfig
}

type YtdlpProxyConfig struct {
	Enable         bool     `toml:"enable" mapstructure:"enable" json:"enable"`
	Sources        []string `toml:"sources" mapstructure:"sources" json:"sources"`
	RefreshMinutes int      `toml:"refresh_minutes" mapstructure:"refresh_minutes" json:"refresh_minutes"`
}
