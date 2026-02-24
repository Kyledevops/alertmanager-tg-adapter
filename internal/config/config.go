package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	TelegramToken           string           `yaml:"telegram_token"`
	ListenAddr              string           `yaml:"listen_addr"`
	ChatID                  int64            `yaml:"chat_id"`
	TemplateFile            string           `yaml:"template_file"`
	AlertmanagerInternalURL string           `yaml:"alertmanager_internal_url"` // 用於 API 調用 (內網)
	AlertmanagerExternalURL string           `yaml:"alertmanager_external_url"` // 用於按鈕跳轉 (外網)
	Routes                  map[string]int64 `yaml:"routes"`                    // Routing based on labels: "label=value": chat_id
}

func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr: ":9087",
		Routes:     make(map[string]int64),
	}

	// Try loading from config.yml first
	configFile := os.Getenv("CONFIG_FILE")
	if configFile == "" {
		configFile = "config.yml"
	}

	data, err := os.ReadFile(configFile)
	if err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %v", err)
		}
	}

	// Override with environment variables
	if token := os.Getenv("TELEGRAM_TOKEN"); token != "" {
		cfg.TelegramToken = token
	}
	if addr := os.Getenv("LISTEN_ADDR"); addr != "" {
		cfg.ListenAddr = addr
	}
	if chatIDStr := os.Getenv("CHAT_ID"); chatIDStr != "" {
		if id, err := strconv.ParseInt(chatIDStr, 10, 64); err == nil {
			cfg.ChatID = id
		}
	}
	if tmpl := os.Getenv("TEMPLATE_FILE"); tmpl != "" {
		cfg.TemplateFile = tmpl
	}
	if amURL := os.Getenv("ALERTMANAGER_INTERNAL_URL"); amURL != "" {
		cfg.AlertmanagerInternalURL = amURL
	}
	if cfg.AlertmanagerInternalURL == "" {
		cfg.AlertmanagerInternalURL = "http://localhost:9093"
	}
	if extURL := os.Getenv("ALERTMANAGER_EXTERNAL_URL"); extURL != "" {
		cfg.AlertmanagerExternalURL = extURL
	}

	// Example env var for routes could be tricky, e.g. ROUTES="env=prod:-100123,env=dev:-100456"
	if routesStr := os.Getenv("ROUTES"); routesStr != "" {
		pairs := strings.Split(routesStr, ",")
		for _, pair := range pairs {
			kp := strings.Split(pair, ":")
			if len(kp) == 2 {
				key := kp[0]
				if id, err := strconv.ParseInt(kp[1], 10, 64); err == nil {
					cfg.Routes[key] = id
				}
			}
		}
	}

	if cfg.TelegramToken == "" {
		return nil, fmt.Errorf("telegram_token is required (in config.yml or TELEGRAM_TOKEN env var)")
	}

	return cfg, nil
}
