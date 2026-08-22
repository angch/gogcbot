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
		"GOGCBOT_TELEGRAM_TOKEN":              "tok_env_123",
		"GOGCBOT_SUPER_ADMIN_ID":              "987654321",
		"GOGCBOT_MODERATION_GROUP_ID":         "-1001234567890",
		"GOGCBOT_DB_PATH":                     "test_env.db",
		"GOGCBOT_LOG_LEVEL":                   "debug",
		"GOGCBOT_CLEANUP_INTERVAL_HOURS":      "5",
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
	if !cfg.Detector.Enabled {
		t.Errorf("Expected Detector.Enabled to be true by default")
	}
	if !cfg.Detector.NewUserCJK.Enabled {
		t.Errorf("Expected Detector.NewUserCJK.Enabled to be true by default")
	}
	if cfg.Detector.NewUserCJK.MinHighUserID != 1000000000 {
		t.Errorf("Expected MinHighUserID 1000000000, got %d", cfg.Detector.NewUserCJK.MinHighUserID)
	}
	if !cfg.Detector.RedPacketName.Enabled {
		t.Errorf("Expected Detector.RedPacketName.Enabled to be true by default")
	}
	if cfg.Detector.RedPacketName.MinHighUserID != 1000000000 {
		t.Errorf("Expected MinHighUserID 1000000000, got %d", cfg.Detector.RedPacketName.MinHighUserID)
	}
	if !cfg.Shieldy.Enabled {
		t.Errorf("Expected Shieldy.Enabled to be true by default")
	}
	if cfg.Shieldy.RepBonus != 5 {
		t.Errorf("Expected Shieldy.RepBonus to be 5 by default, got %d", cfg.Shieldy.RepBonus)
	}
	if cfg.Shieldy.MaxMessages != 5 {
		t.Errorf("Expected Shieldy.MaxMessages to be 5 by default, got %d", cfg.Shieldy.MaxMessages)
	}
}

func TestLoadConfig_LegacyNewUserChineseCompatibility(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "legacy_config.yaml")

	content := `
detector:
  enabled: true
  new_user_chinese:
    enabled: true
    min_high_user_id: 2000000000
    max_reputation: 5
    rep_penalty: 35
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp config file: %v", err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if !cfg.Detector.NewUserCJK.Enabled {
		t.Errorf("Expected NewUserCJK to be enabled from legacy new_user_chinese config")
	}
	if cfg.Detector.NewUserCJK.MinHighUserID != 2000000000 {
		t.Errorf("Expected MinHighUserID 2000000000, got %d", cfg.Detector.NewUserCJK.MinHighUserID)
	}
	if cfg.Detector.NewUserCJK.MaxReputation != 5 {
		t.Errorf("Expected MaxReputation 5, got %d", cfg.Detector.NewUserCJK.MaxReputation)
	}
	if cfg.Detector.NewUserCJK.RepPenalty != 35 {
		t.Errorf("Expected RepPenalty 35, got %d", cfg.Detector.NewUserCJK.RepPenalty)
	}
}

func TestLoadConfig_RedPacketNameConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "redpacket_config.yaml")

	content := `
detector:
  enabled: true
  red_packet_name:
    enabled: true
    min_high_user_id: 3000000000
    max_reputation: 3
    min_username_length: 6
    rep_penalty: 25
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp config file: %v", err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if !cfg.Detector.RedPacketName.Enabled {
		t.Errorf("Expected RedPacketName to be enabled")
	}
	if cfg.Detector.RedPacketName.MinHighUserID != 3000000000 {
		t.Errorf("Expected MinHighUserID 3000000000, got %d", cfg.Detector.RedPacketName.MinHighUserID)
	}
	if cfg.Detector.RedPacketName.MaxReputation != 3 {
		t.Errorf("Expected MaxReputation 3, got %d", cfg.Detector.RedPacketName.MaxReputation)
	}
	if cfg.Detector.RedPacketName.MinUsernameLength != 6 {
		t.Errorf("Expected MinUsernameLength 6, got %d", cfg.Detector.RedPacketName.MinUsernameLength)
	}
	if cfg.Detector.RedPacketName.RepPenalty != 25 {
		t.Errorf("Expected RepPenalty 25, got %d", cfg.Detector.RedPacketName.RepPenalty)
	}
}

func TestLoadConfig_LegacyNewUserRedPacketCompatibility(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "legacy_redpacket_config.yaml")

	content := `
detector:
  enabled: true
  new_user_red_packet:
    enabled: true
    min_high_user_id: 4000000000
    max_reputation: 2
    rep_penalty: 40
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp config file: %v", err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if !cfg.Detector.RedPacketName.Enabled {
		t.Errorf("Expected RedPacketName to be enabled from legacy new_user_red_packet config")
	}
	if cfg.Detector.RedPacketName.MinHighUserID != 4000000000 {
		t.Errorf("Expected MinHighUserID 4000000000, got %d", cfg.Detector.RedPacketName.MinHighUserID)
	}
	if cfg.Detector.RedPacketName.MaxReputation != 2 {
		t.Errorf("Expected MaxReputation 2, got %d", cfg.Detector.RedPacketName.MaxReputation)
	}
	if cfg.Detector.RedPacketName.RepPenalty != 40 {
		t.Errorf("Expected RepPenalty 40, got %d", cfg.Detector.RedPacketName.RepPenalty)
	}
}
