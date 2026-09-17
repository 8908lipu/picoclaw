package config

import (
	"path/filepath"
	"testing"
)

func TestDynamicEnvironmentOverrides(t *testing.T) {
	// Set environment variables
	t.Setenv("PORT", "10000")
	t.Setenv("CUSTOM_MODEL_NAME", "my-render-ai")
	t.Setenv("CUSTOM_MODEL_PROVIDER", "openai")
	t.Setenv("CUSTOM_MODEL_BASE_URL", "https://api.mycustomai.com/v1")
	t.Setenv("CUSTOM_MODEL_API_KEY", "sk-custom-secret-key")
	t.Setenv("TELEGRAM_BOT_TOKEN", "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11")
	t.Setenv("TELEGRAM_ALLOWED_USERS", "1234567, 8901234")

	tmpDir := t.TempDir()
	nonExistentConfig := filepath.Join(tmpDir, "non_existent_config.json")

	cfg, err := LoadConfig(nonExistentConfig)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	// 1. Verify Port
	if cfg.Gateway.Port != 10000 {
		t.Errorf("expected Gateway.Port 10000, got %d", cfg.Gateway.Port)
	}
	if cfg.Gateway.Host != "0.0.0.0" {
		t.Errorf("expected Gateway.Host 0.0.0.0, got %q", cfg.Gateway.Host)
	}

	// 2. Verify Custom Model
	if cfg.Agents.Defaults.ModelName != "my-render-ai" {
		t.Errorf("expected Defaults.ModelName my-render-ai, got %q", cfg.Agents.Defaults.ModelName)
	}
	if len(cfg.ModelList) == 0 {
		t.Fatalf("expected ModelList to contain custom model, got empty")
	}
	foundModel := false
	for _, m := range cfg.ModelList {
		if m.ModelName == "my-render-ai" {
			foundModel = true
			if m.APIBase != "https://api.mycustomai.com/v1" {
				t.Errorf("expected APIBase https://api.mycustomai.com/v1, got %q", m.APIBase)
			}
			if m.APIKey() != "sk-custom-secret-key" {
				t.Errorf("expected APIKey sk-custom-secret-key, got %q", m.APIKey())
			}
			if !m.Enabled {
				t.Errorf("expected custom model to be enabled")
			}
			break
		}
	}
	if !foundModel {
		t.Errorf("custom model my-render-ai not found in ModelList")
	}

	// 3. Verify Telegram Channel
	tgChannel := cfg.Channels.Get(ChannelTelegram)
	if tgChannel == nil {
		t.Fatalf("expected Telegram channel to be present")
	}
	if !tgChannel.Enabled {
		t.Errorf("expected Telegram channel to be enabled")
	}
	decoded, err := tgChannel.GetDecoded()
	if err != nil {
		t.Fatalf("GetDecoded() error = %v", err)
	}
	settings, ok := decoded.(*TelegramSettings)
	if !ok {
		t.Fatalf("expected *TelegramSettings, got %T", decoded)
	}
	if settings.Token.String() != "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11" {
		t.Errorf("expected Token 123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11, got %q", settings.Token.String())
	}
	if len(tgChannel.AllowFrom) != 2 || tgChannel.AllowFrom[0] != "1234567" || tgChannel.AllowFrom[1] != "8901234" {
		t.Errorf("unexpected AllowFrom: %+v", tgChannel.AllowFrom)
	}
}
