// Package config manages configuration structure, defaults, Viper loading, and YAML serialization.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/angch/gogcbot/pkg/detector"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Config represents the top-level application configuration structure.
type Config struct {
	TelegramToken     string           `mapstructure:"telegram_token" yaml:"telegram_token"`
	SuperAdminID      int64            `mapstructure:"super_admin_id" yaml:"super_admin_id"`
	ModerationGroupID int64            `mapstructure:"moderation_group_id" yaml:"moderation_group_id"`
	DBPath            string           `mapstructure:"db_path" yaml:"db_path"`
	LogLevel          string           `mapstructure:"log_level" yaml:"log_level"`
	AutoFlag          AutoFlagConfig   `mapstructure:"auto_flag" yaml:"auto_flag"`
	Reputation        ReputationConfig `mapstructure:"reputation" yaml:"reputation"`
	Detector          DetectorConfig   `mapstructure:"detector" yaml:"detector"`
	Shieldy           ShieldyConfig    `mapstructure:"shieldy" yaml:"shieldy"`
	CleanupIntervalHr int              `mapstructure:"cleanup_interval_hours" yaml:"cleanup_interval_hours"`
}

// ShieldyConfig defines settings for Shieldy captcha bot verification.
type ShieldyConfig struct {
	Enabled             bool `mapstructure:"enabled" yaml:"enabled"`
	RepBonus            int  `mapstructure:"rep_bonus" yaml:"rep_bonus"`
	MaxMessages         int  `mapstructure:"max_messages" yaml:"max_messages"`
	RecheckDelayMinutes int  `mapstructure:"recheck_delay_minutes" yaml:"recheck_delay_minutes"`
}

// DetectorConfig defines settings for modular detection triggers.
type DetectorConfig struct {
	Enabled               bool                                  `mapstructure:"enabled" yaml:"enabled"`
	NewUserCJK            detector.NewUserCJKTriggerConfig      `mapstructure:"new_user_cjk" yaml:"new_user_cjk"`
	NewUserChinese        detector.NewUserCJKTriggerConfig      `mapstructure:"new_user_chinese" yaml:"new_user_chinese,omitempty"`
	UsernameAnomaly       detector.UsernameAnomalyTriggerConfig `mapstructure:"username_anomaly" yaml:"username_anomaly,omitempty"`
	ProfileNameKeywordBan detector.ProfileNameKeywordBanConfig  `mapstructure:"profile_name_keyword_ban" yaml:"profile_name_keyword_ban,omitempty"`
	OllamaNameClassifier  detector.OllamaNameClassifierConfig   `mapstructure:"ollama_name_classifier" yaml:"ollama_name_classifier,omitempty"`
}

// AutoFlagConfig defines automated moderation rules and keyword detection thresholds.
type AutoFlagConfig struct {
	Enabled         bool     `mapstructure:"enabled" yaml:"enabled"`
	LowRepThreshold int      `mapstructure:"low_rep_threshold" yaml:"low_rep_threshold"`
	FlagOnLinks     bool     `mapstructure:"flag_on_links" yaml:"flag_on_links"`
	NewUserMinPosts int      `mapstructure:"new_user_min_posts" yaml:"new_user_min_posts"`
	BlockedKeywords []string `mapstructure:"blocked_keywords" yaml:"blocked_keywords"`
}

// ReputationConfig defines point deltas and penalties for reputation adjustments.
type ReputationConfig struct {
	DefaultInitial int `mapstructure:"default_initial" yaml:"default_initial"`
	FlagThreshold  int `mapstructure:"flag_threshold" yaml:"flag_threshold"`
	ApproveBonus   int `mapstructure:"approve_bonus" yaml:"approve_bonus"`
	DeletePenalty  int `mapstructure:"delete_penalty" yaml:"delete_penalty"`
	WarnPenalty    int `mapstructure:"warn_penalty" yaml:"warn_penalty"`
	MutePenalty    int `mapstructure:"mute_penalty" yaml:"mute_penalty"`
	BanPenalty     int `mapstructure:"ban_penalty" yaml:"ban_penalty"`
}

// DefaultConfig returns the standard fallback configuration values.
func DefaultConfig() Config {
	return Config{
		TelegramToken:     "",
		SuperAdminID:      0,
		ModerationGroupID: 0,
		DBPath:            "gogcbot.db",
		LogLevel:          "info",
		AutoFlag: AutoFlagConfig{
			Enabled:         true,
			LowRepThreshold: 50,
			FlagOnLinks:     true,
			NewUserMinPosts: 3,
			BlockedKeywords: []string{
				"crypto giveaway",
				"t.me/",
				"whatsapp.com",
				"fast money",
				"airdrop",
				"free rolls",
			},
		},
		Reputation: ReputationConfig{
			DefaultInitial: 0,
			FlagThreshold:  40,
			ApproveBonus:   5,
			DeletePenalty:  10,
			WarnPenalty:    20,
			MutePenalty:    30,
			BanPenalty:     50,
		},
		Detector: DetectorConfig{
			Enabled: true,
			NewUserCJK: detector.NewUserCJKTriggerConfig{
				Enabled:       true,
				MinHighUserID: 1000000000,
				MaxReputation: 5,
				MaxUserPosts:  5,
				RepPenalty:    20,
			},
			UsernameAnomaly: detector.UsernameAnomalyTriggerConfig{
				Enabled:       true,
				MinHighUserID: 1000000000,
				MaxReputation: 5,
				MaxUserPosts:  5,
				MinScore:      3,
				FlagOnly:      true,
				RepPenalty:    20,
			},
			ProfileNameKeywordBan: detector.ProfileNameKeywordBanConfig{
				Enabled:         true,
				MinHighUserID:   1000000000,
				MaxReputation:   5,
				MaxUserPosts:    5,
				MinScore:        3,
				FlagOnly:        true,
				RepPenalty:      20,
				BlockedKeywords: []string{"0壹天", "每日", "吴压", "吾思", "兼织"},
			},
			OllamaNameClassifier: detector.OllamaNameClassifierConfig{
				Enabled:        true,
				OllamaURL:      "http://localhost:11434",
				Model:          "phi4",
				MinHighUserID:  1000000000,
				MaxReputation:  5,
				MaxUserPosts:   5,
				RequestTimeout: 10 * time.Second,
				FlagOnly:       true,
				RepPenalty:     20,
			},
		},
		Shieldy: ShieldyConfig{
			Enabled:             true,
			RepBonus:            5,
			MaxMessages:         5,
			RecheckDelayMinutes: 6,
		},
		CleanupIntervalHr: 1,
	}
}

func setViperDefaults(v *viper.Viper) {
	v.SetDefault("telegram_token", "")
	v.SetDefault("super_admin_id", int64(0))
	v.SetDefault("moderation_group_id", int64(0))
	v.SetDefault("db_path", "gogcbot.db")
	v.SetDefault("log_level", "info")
	v.SetDefault("cleanup_interval_hours", 1)

	v.SetDefault("auto_flag.enabled", true)
	v.SetDefault("auto_flag.low_rep_threshold", 50)
	v.SetDefault("auto_flag.flag_on_links", true)
	v.SetDefault("auto_flag.new_user_min_posts", 3)
	v.SetDefault("auto_flag.blocked_keywords", []string{
		"crypto giveaway",
		"t.me/",
		"whatsapp.com",
		"fast money",
		"airdrop",
		"free rolls",
	})

	v.SetDefault("reputation.default_initial", 0)
	v.SetDefault("reputation.flag_threshold", 40)
	v.SetDefault("reputation.approve_bonus", 5)
	v.SetDefault("reputation.delete_penalty", 10)
	v.SetDefault("reputation.warn_penalty", 20)
	v.SetDefault("reputation.mute_penalty", 30)
	v.SetDefault("reputation.ban_penalty", 50)

	v.SetDefault("detector.enabled", true)
	v.SetDefault("detector.new_user_cjk.enabled", true)
	v.SetDefault("detector.new_user_cjk.min_high_user_id", int64(1000000000))
	v.SetDefault("detector.new_user_cjk.max_reputation", 5)
	v.SetDefault("detector.new_user_cjk.max_user_posts", 5)
	v.SetDefault("detector.new_user_cjk.rep_penalty", 20)

	v.SetDefault("detector.new_user_chinese.enabled", true)
	v.SetDefault("detector.new_user_chinese.min_high_user_id", int64(1000000000))
	v.SetDefault("detector.new_user_chinese.max_reputation", 5)
	v.SetDefault("detector.new_user_chinese.max_user_posts", 5)
	v.SetDefault("detector.new_user_chinese.rep_penalty", 20)

	v.SetDefault("detector.username_anomaly.enabled", true)
	v.SetDefault("detector.username_anomaly.min_high_user_id", int64(1000000000))
	v.SetDefault("detector.username_anomaly.max_reputation", 5)
	v.SetDefault("detector.username_anomaly.max_user_posts", 5)
	v.SetDefault("detector.username_anomaly.min_score", 3)
	v.SetDefault("detector.username_anomaly.flag_only", true)
	v.SetDefault("detector.username_anomaly.rep_penalty", 20)

	v.SetDefault("detector.profile_name_keyword_ban.enabled", true)
	v.SetDefault("detector.profile_name_keyword_ban.min_high_user_id", int64(1000000000))
	v.SetDefault("detector.profile_name_keyword_ban.max_reputation", 5)
	v.SetDefault("detector.profile_name_keyword_ban.max_user_posts", 5)
	v.SetDefault("detector.profile_name_keyword_ban.min_score", 3)
	v.SetDefault("detector.profile_name_keyword_ban.flag_only", true)
	v.SetDefault("detector.profile_name_keyword_ban.rep_penalty", 20)
	v.SetDefault("detector.profile_name_keyword_ban.blocked_keywords", []string{"0壹天", "每日", "吴压", "吾思", "兼织"})

	v.SetDefault("detector.ollama_name_classifier.enabled", true)
	v.SetDefault("detector.ollama_name_classifier.ollama_url", "http://localhost:11434")
	v.SetDefault("detector.ollama_name_classifier.model", "phi4")
	v.SetDefault("detector.ollama_name_classifier.min_high_user_id", int64(1000000000))
	v.SetDefault("detector.ollama_name_classifier.max_reputation", 5)
	v.SetDefault("detector.ollama_name_classifier.max_user_posts", 5)
	v.SetDefault("detector.ollama_name_classifier.request_timeout", "10s")
	v.SetDefault("detector.ollama_name_classifier.flag_only", true)
	v.SetDefault("detector.ollama_name_classifier.rep_penalty", 20)

	v.SetDefault("shieldy.enabled", true)
	v.SetDefault("shieldy.rep_bonus", 5)
	v.SetDefault("shieldy.max_messages", 5)
	v.SetDefault("shieldy.recheck_delay_minutes", 6)
}

// LoadConfig reads configuration from file or environment variables via Viper.
func LoadConfig(cfgFile string) (*Config, error) {
	v := viper.New()

	v.SetEnvPrefix("GOGCBOT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	setViperDefaults(v)

	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		v.AddConfigPath(".")
		v.AddConfigPath("./config")
		v.SetConfigName("config")
		v.SetConfigType("yaml")
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok && !os.IsNotExist(err) {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	cfg := DefaultConfig()
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unable to decode config into struct: %w", err)
	}

	if (v.InConfig("detector.new_user_chinese") || cfg.Detector.NewUserChinese.MinHighUserID != 0 || cfg.Detector.NewUserChinese.RepPenalty != 0) && !v.InConfig("detector.new_user_cjk") {
		cfg.Detector.NewUserCJK = cfg.Detector.NewUserChinese
	}

	return &cfg, nil
}

// SaveDefaultConfig writes a standard default configuration to the specified YAML file path.
func SaveDefaultConfig(filePath string) error {
	cfg := DefaultConfig()
	return SaveConfig(filePath, &cfg)
}

// SaveConfig serializes the provided Config struct to YAML format and saves it to disk.
func SaveConfig(filePath string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	dir := filepath.Dir(filePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
