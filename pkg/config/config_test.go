package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_EnvVar(t *testing.T) {
	testToken := "123456789:ABCdefGHIjklMNOpqrsTUVwxyz"
	os.Setenv("GOGCBOT_TELEGRAM_TOKEN", testToken)
	defer os.Unsetenv("GOGCBOT_TELEGRAM_TOKEN")

	// Test when config file does not exist
	cfg, err := LoadConfig("non_existent_config.yaml")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.TelegramToken != testToken {
		t.Errorf("Expected TelegramToken to be %q, got %q", testToken, cfg.TelegramToken)
	}
}

func TestLoadConfig_EnvVarOverridesConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	content := `telegram_token: "file_token_abc"`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp config file: %v", err)
	}

	testToken := "123456789:env_token"
	os.Setenv("GOGCBOT_TELEGRAM_TOKEN", testToken)
	defer os.Unsetenv("GOGCBOT_TELEGRAM_TOKEN")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.TelegramToken != testToken {
		t.Errorf("Expected TelegramToken to be %q, got %q", testToken, cfg.TelegramToken)
	}
}

func TestLoadConfig_EnvVarKeyMissingFromConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	content := `db_path: "custom.db"`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp config file: %v", err)
	}

	testToken := "123456789:env_token_missing_in_file"
	os.Setenv("GOGCBOT_TELEGRAM_TOKEN", testToken)
	defer os.Unsetenv("GOGCBOT_TELEGRAM_TOKEN")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.TelegramToken != testToken {
		t.Errorf("Expected TelegramToken to be %q, got %q", testToken, cfg.TelegramToken)
	}
}

func TestLoadConfig_AllEnvVars(t *testing.T) {
	envs := map[string]string{
		"GOGCBOT_TELEGRAM_TOKEN":          "tok_env_123",
		"GOGCBOT_SUPER_ADMIN_ID":          "987654321",
		"GOGCBOT_MODERATION_GROUP_ID":     "-1001234567890",
		"GOGCBOT_DB_PATH":                 "test_env.db",
		"GOGCBOT_LOG_LEVEL":               "debug",
		"GOGCBOT_CLEANUP_INTERVAL_HOURS":  "5",
		"GOGCBOT_AUTO_FLAG_LOW_REP_THRESHOLD": "30",
		"GOGCBOT_REPUTATION_DEFAULT_INITIAL":  "150",
	}

	for k, v := range envs {
		os.Setenv(k, v)
		defer os.Unsetenv(k)
	}

	cfg, err := LoadConfig("non_existent_config.yaml")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.TelegramToken != "tok_env_123" {
		t.Errorf("Expected TelegramToken tok_env_123, got %q", cfg.TelegramToken)
	}
	if cfg.SuperAdminID != 987654321 {
		t.Errorf("Expected SuperAdminID 987654321, got %d", cfg.SuperAdminID)
	}
	if cfg.ModerationGroupID != -1001234567890 {
		t.Errorf("Expected ModerationGroupID -1001234567890, got %d", cfg.ModerationGroupID)
	}
	if cfg.DBPath != "test_env.db" {
		t.Errorf("Expected DBPath test_env.db, got %q", cfg.DBPath)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("Expected LogLevel debug, got %q", cfg.LogLevel)
	}
	if cfg.CleanupIntervalHr != 5 {
		t.Errorf("Expected CleanupIntervalHr 5, got %d", cfg.CleanupIntervalHr)
	}
	if cfg.AutoFlag.LowRepThreshold != 30 {
		t.Errorf("Expected AutoFlag.LowRepThreshold 30, got %d", cfg.AutoFlag.LowRepThreshold)
	}
	if cfg.Reputation.DefaultInitial != 150 {
		t.Errorf("Expected Reputation.DefaultInitial 150, got %d", cfg.Reputation.DefaultInitial)
	}
}

func TestSaveConfig_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig()
	cfg.SuperAdminID = 99887766
	cfg.ModerationGroupID = -10099887766

	if err := SaveConfig(cfgPath, &cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	loaded, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if loaded.SuperAdminID != 99887766 {
		t.Errorf("Expected SuperAdminID 99887766, got %d", loaded.SuperAdminID)
	}
	if loaded.ModerationGroupID != -10099887766 {
		t.Errorf("Expected ModerationGroupID -10099887766, got %d", loaded.ModerationGroupID)
	}
}

func TestDefaultConfig_DefaultInitialZero(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Reputation.DefaultInitial != 0 {
		t.Errorf("Expected DefaultInitial 0, got %d", cfg.Reputation.DefaultInitial)
	}
}
