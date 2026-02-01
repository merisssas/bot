package config

type userSyncConfig struct {
	DeleteMissing bool `toml:"delete_missing" mapstructure:"delete_missing" json:"delete_missing"`
}
