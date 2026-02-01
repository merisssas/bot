package config

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func RegisterFlags(cmd *cobra.Command) {
	flags := cmd.Flags()

	// Base configuration
	flags.StringP("config", "c", "", "config file path")
	flags.StringP("lang", "l", "", "language (e.g., en)")
	flags.IntP("workers", "w", 0, "number of workers")
	flags.Int("retry", 0, "retry times")
	flags.Int("threads", 0, "number of threads")
	flags.Bool("stream", false, "enable stream mode")
	flags.Bool("no-clean-cache", false, "do not clean cache on exit")
	flags.String("proxy", "", "proxy URL (http, https, socks5, socks5h)")

	// Telegram configuration
	flags.String("telegram-token", "", "telegram bot token")
	flags.Int("telegram-app-id", 0, "telegram app id")
	flags.String("telegram-app-hash", "", "telegram app hash")
	flags.Int("telegram-rpc-retry", 0, "telegram rpc retry times")
	flags.Bool("telegram-userbot-enable", false, "enable userbot")
	flags.String("telegram-userbot-session", "", "userbot session path")
	flags.Bool("telegram-proxy-enable", false, "enable telegram proxy")
	flags.String("telegram-proxy-url", "", "telegram proxy URL")

	// Database configuration
	flags.String("db-path", "", "database path")
	flags.String("db-session", "", "session database path")

	// Temp directory configuration
	flags.String("temp-base-path", "", "temp directory base path")

	// Parser configuration
	flags.Bool("parser-plugin-enable", false, "enable parser plugins")
	flags.StringSlice("parser-plugin-dirs", nil, "parser plugin directories")
	flags.String("parser-proxy", "", "parser proxy URL")

	// Directlinks configuration
	flags.Int("directlinks-max-concurrency", 0, "directlinks: max concurrent downloads per task")
	flags.Int("directlinks-segment-concurrency", 0, "directlinks: max concurrent segments per file")
	flags.String("directlinks-min-multipart-size", "", "directlinks: minimum file size for multipart (e.g. 5MB)")
	flags.String("directlinks-min-segment-size", "", "directlinks: minimum segment size (e.g. 1MB)")
	flags.Bool("directlinks-enable-resume", false, "directlinks: enable resume support")
	flags.Int("directlinks-max-retries", 0, "directlinks: max retries per request")
	flags.Duration("directlinks-retry-base-delay", 0, "directlinks: retry base delay (e.g. 500ms)")
	flags.Duration("directlinks-retry-max-delay", 0, "directlinks: retry max delay (e.g. 10s)")
	flags.String("directlinks-limit-rate", "", "directlinks: bandwidth limit (e.g. 10M)")
	flags.String("directlinks-burst-rate", "", "directlinks: burst bandwidth allowance (e.g. 2M)")
	flags.Bool("directlinks-dry-run", false, "directlinks: dry run mode")
	flags.String("directlinks-overwrite-policy", "", "directlinks: overwrite policy (overwrite|rename|skip)")
	flags.String("directlinks-checksum-algorithm", "", "directlinks: checksum algorithm (sha256, sha1, md5)")
	flags.String("directlinks-expected-checksum", "", "directlinks: expected checksum")
	flags.Bool("directlinks-write-checksum-file", false, "directlinks: write checksum file")
	flags.String("directlinks-log-file", "", "directlinks: log file path")
	flags.String("directlinks-log-level", "", "directlinks: log level (debug|info|warn|error)")
	flags.String("directlinks-user-agent", "", "directlinks: user agent")
	flags.String("directlinks-proxy", "", "directlinks: proxy URL")
	flags.String("directlinks-auth-username", "", "directlinks: basic auth username")
	flags.String("directlinks-auth-password", "", "directlinks: basic auth password")
	flags.Int("directlinks-default-priority", 0, "directlinks: default queue priority")

	// Bind to viper
	bindFlags(cmd)
}

func bindFlags(cmd *cobra.Command) {
	flags := cmd.Flags()

	viper.BindPFlag("lang", flags.Lookup("lang"))
	viper.BindPFlag("workers", flags.Lookup("workers"))
	viper.BindPFlag("retry", flags.Lookup("retry"))
	viper.BindPFlag("threads", flags.Lookup("threads"))
	viper.BindPFlag("stream", flags.Lookup("stream"))
	viper.BindPFlag("no_clean_cache", flags.Lookup("no-clean-cache"))
	viper.BindPFlag("proxy", flags.Lookup("proxy"))

	// Telegram
	viper.BindPFlag("telegram.token", flags.Lookup("telegram-token"))
	viper.BindPFlag("telegram.app_id", flags.Lookup("telegram-app-id"))
	viper.BindPFlag("telegram.app_hash", flags.Lookup("telegram-app-hash"))
	viper.BindPFlag("telegram.rpc_retry", flags.Lookup("telegram-rpc-retry"))
	viper.BindPFlag("telegram.userbot.enable", flags.Lookup("telegram-userbot-enable"))
	viper.BindPFlag("telegram.userbot.session", flags.Lookup("telegram-userbot-session"))
	viper.BindPFlag("telegram.proxy.enable", flags.Lookup("telegram-proxy-enable"))
	viper.BindPFlag("telegram.proxy.url", flags.Lookup("telegram-proxy-url"))

	// database
	viper.BindPFlag("db.path", flags.Lookup("db-path"))
	viper.BindPFlag("db.session", flags.Lookup("db-session"))
	// Temp directory
	viper.BindPFlag("temp.base_path", flags.Lookup("temp-base-path"))

	// Parser
	viper.BindPFlag("parser.plugin_enable", flags.Lookup("parser-plugin-enable"))
	viper.BindPFlag("parser.plugin_dirs", flags.Lookup("parser-plugin-dirs"))
	viper.BindPFlag("parser.proxy", flags.Lookup("parser-proxy"))

	// Directlinks
	viper.BindPFlag("directlinks.max_concurrency", flags.Lookup("directlinks-max-concurrency"))
	viper.BindPFlag("directlinks.segment_concurrency", flags.Lookup("directlinks-segment-concurrency"))
	viper.BindPFlag("directlinks.min_multipart_size", flags.Lookup("directlinks-min-multipart-size"))
	viper.BindPFlag("directlinks.min_segment_size", flags.Lookup("directlinks-min-segment-size"))
	viper.BindPFlag("directlinks.enable_resume", flags.Lookup("directlinks-enable-resume"))
	viper.BindPFlag("directlinks.max_retries", flags.Lookup("directlinks-max-retries"))
	viper.BindPFlag("directlinks.retry_base_delay", flags.Lookup("directlinks-retry-base-delay"))
	viper.BindPFlag("directlinks.retry_max_delay", flags.Lookup("directlinks-retry-max-delay"))
	viper.BindPFlag("directlinks.limit_rate", flags.Lookup("directlinks-limit-rate"))
	viper.BindPFlag("directlinks.burst_rate", flags.Lookup("directlinks-burst-rate"))
	viper.BindPFlag("directlinks.dry_run", flags.Lookup("directlinks-dry-run"))
	viper.BindPFlag("directlinks.overwrite_policy", flags.Lookup("directlinks-overwrite-policy"))
	viper.BindPFlag("directlinks.checksum_algorithm", flags.Lookup("directlinks-checksum-algorithm"))
	viper.BindPFlag("directlinks.expected_checksum", flags.Lookup("directlinks-expected-checksum"))
	viper.BindPFlag("directlinks.write_checksum_file", flags.Lookup("directlinks-write-checksum-file"))
	viper.BindPFlag("directlinks.log_file", flags.Lookup("directlinks-log-file"))
	viper.BindPFlag("directlinks.log_level", flags.Lookup("directlinks-log-level"))
	viper.BindPFlag("directlinks.user_agent", flags.Lookup("directlinks-user-agent"))
	viper.BindPFlag("directlinks.proxy", flags.Lookup("directlinks-proxy"))
	viper.BindPFlag("directlinks.auth_username", flags.Lookup("directlinks-auth-username"))
	viper.BindPFlag("directlinks.auth_password", flags.Lookup("directlinks-auth-password"))
	viper.BindPFlag("directlinks.default_priority", flags.Lookup("directlinks-default-priority"))
}

func GetConfigFile(cmd *cobra.Command) string {
	configFile, _ := cmd.Flags().GetString("config")
	return configFile
}
