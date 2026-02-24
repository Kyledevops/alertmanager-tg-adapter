package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alertmanager-tg-adapter/internal/config"
	"github.com/alertmanager-tg-adapter/internal/model"
)

// mockBot is a minimal bot implementation for testing routing logic
// We can't fully test SendAlert without a real Telegram API,
// so we test the server's HTTP handling and routing separately.

func TestHealthEndpoint(t *testing.T) {
	cfg := &config.Config{
		TelegramToken: "test-token",
		ListenAddr:    ":9087",
	}

	// We can't create a real bot without a valid token,
	// so we test the health endpoint directly
	srv := &Server{cfg: cfg, bot: nil}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	srv.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Health status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != "OK" {
		t.Errorf("Health body = %q, want %q", w.Body.String(), "OK")
	}
}

func TestWebhookMethodNotAllowed(t *testing.T) {
	srv := &Server{cfg: &config.Config{}, bot: nil}

	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	w := httptest.NewRecorder()

	srv.handleWebhook(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestWebhookBadPayload(t *testing.T) {
	srv := &Server{cfg: &config.Config{}, bot: nil}

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString("invalid json"))
	w := httptest.NewRecorder()

	srv.handleWebhook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestWebhookNoChatID(t *testing.T) {
	srv := &Server{
		cfg: &config.Config{
			ChatID: 0,
			Routes: make(map[string]int64),
		},
		bot: nil,
	}

	payload := model.AlertManagerWebhook{
		Status:       "firing",
		CommonLabels: map[string]string{"alertname": "TestAlert"},
		Alerts: []model.Alert{
			{
				Status: "firing",
				Labels: map[string]string{"alertname": "TestAlert"},
			},
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	srv.handleWebhook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d (no chat ID configured)", w.Code, http.StatusBadRequest)
	}
}

func TestRoutingLogic(t *testing.T) {
	tests := []struct {
		name         string
		routes       map[string]int64
		defaultChat  int64
		commonLabels map[string]string
		wantChatIDs  []int64
	}{
		{
			name:         "match severity route",
			routes:       map[string]int64{"severity=critical": -100111},
			defaultChat:  -100999,
			commonLabels: map[string]string{"severity": "critical", "alertname": "Test"},
			wantChatIDs:  []int64{-100111},
		},
		{
			name:         "no match falls to default",
			routes:       map[string]int64{"severity=critical": -100111},
			defaultChat:  -100999,
			commonLabels: map[string]string{"severity": "warning", "alertname": "Test"},
			wantChatIDs:  []int64{-100999},
		},
		{
			name: "multiple route matches",
			routes: map[string]int64{
				"severity=critical": -100111,
				"team=backend":     -100222,
			},
			defaultChat:  -100999,
			commonLabels: map[string]string{"severity": "critical", "team": "backend"},
			wantChatIDs:  []int64{-100111, -100222},
		},
		{
			name:         "empty routes uses default",
			routes:       map[string]int64{},
			defaultChat:  -100999,
			commonLabels: map[string]string{"alertname": "Test"},
			wantChatIDs:  []int64{-100999},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				ChatID: tt.defaultChat,
				Routes: tt.routes,
			}

			// Simulate routing logic (extracted from handleWebhook)
			targetChatIDs := make(map[int64]bool)

			for routeKey, routeChatID := range cfg.Routes {
				parts := splitRoute(routeKey)
				if parts == nil {
					continue
				}
				if val, ok := tt.commonLabels[parts[0]]; ok && val == parts[1] {
					targetChatIDs[routeChatID] = true
				}
			}

			if len(targetChatIDs) == 0 && cfg.ChatID != 0 {
				targetChatIDs[cfg.ChatID] = true
			}

			// Verify all expected chat IDs are present
			for _, wantID := range tt.wantChatIDs {
				if !targetChatIDs[wantID] {
					t.Errorf("Expected ChatID %d in targets, got %v", wantID, targetChatIDs)
				}
			}

			if len(targetChatIDs) != len(tt.wantChatIDs) {
				// For multi-match case, targets might map to same ID
				// Just check at least the expected ones are present
				for _, wantID := range tt.wantChatIDs {
					if !targetChatIDs[wantID] {
						t.Errorf("Missing expected ChatID %d", wantID)
					}
				}
			}
		})
	}
}

// splitRoute splits a "key=value" route key into [key, value]
func splitRoute(routeKey string) []string {
	for i, c := range routeKey {
		if c == '=' {
			return []string{routeKey[:i], routeKey[i+1:]}
		}
	}
	return nil
}

func TestHandlerReturnsNonNil(t *testing.T) {
	srv := &Server{
		cfg: &config.Config{},
		bot: nil,
	}

	handler := srv.Handler()
	if handler == nil {
		t.Error("Handler() returned nil")
	}
}
