package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/alertmanager-tg-adapter/internal/bot"
	"github.com/alertmanager-tg-adapter/internal/config"
	"github.com/alertmanager-tg-adapter/internal/model"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	alertsReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "alert_telegram_adapter_received_total",
			Help: "Total number of alerts received.",
		},
		[]string{"status"},
	)
	alertsSent = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "alert_telegram_adapter_sent_total",
			Help: "Total number of alerts forwarded to Telegram.",
		},
		[]string{"status", "chat_id"},
	)
	alertsSendFailed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "alert_telegram_adapter_send_failed_total",
			Help: "Total number of alerts that failed to send to Telegram.",
		},
		[]string{"status", "chat_id"},
	)
	requestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "alert_telegram_adapter_request_duration_seconds",
			Help:    "Duration of HTTP requests.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"path"},
	)
)

func init() {
	prometheus.MustRegister(alertsReceived)
	prometheus.MustRegister(alertsSent)
	prometheus.MustRegister(alertsSendFailed)
	prometheus.MustRegister(requestDuration)
}

type Server struct {
	cfg *config.Config
	bot *bot.Bot
}

func New(cfg *config.Config, b *bot.Bot) *Server {
	return &Server{
		cfg: cfg,
		bot: b,
	}
}

// Handler returns the HTTP handler (for graceful shutdown support)
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", s.handleWebhook)
	mux.HandleFunc("/health", s.handleHealth)

	// Expose current metrics
	mux.Handle("/metrics", promhttp.Handler())

	// Wrap mux with logging middleware
	return s.loggingMiddleware(mux)
}

// Run starts the HTTP server (kept for backward compatibility)
func (s *Server) Run(addr string) error {
	return http.ListenAndServe(addr, s.Handler())
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timer := prometheus.NewTimer(requestDuration.WithLabelValues(r.URL.Path))
		defer timer.ObserveDuration()

		// Skip logging for health check
		if r.URL.Path == "/health" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		log.Printf("📨 %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var alertPayload model.AlertManagerWebhook
	if err := json.NewDecoder(r.Body).Decode(&alertPayload); err != nil {
		log.Printf("❌ Error decoding webhook payload: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Log received alert summary
	log.Printf("🔔 Received alert: Status=%s, Alerts=%d, GroupKey=%s",
		alertPayload.Status, len(alertPayload.Alerts), alertPayload.GroupKey)

	// Update metrics
	alertsReceived.WithLabelValues(alertPayload.Status).Inc()

	// Determine Chat ID based on Routing
	targetChatIDs := make(map[int64]bool)

	// Check routes
	for routeKey, routeChatID := range s.cfg.Routes {
		parts := strings.SplitN(routeKey, "=", 2)
		if len(parts) != 2 {
			continue
		}
		labelName := parts[0]
		labelValue := parts[1]

		// Check CommonLabels
		if val, ok := alertPayload.CommonLabels[labelName]; ok && val == labelValue {
			targetChatIDs[routeChatID] = true
		}
	}

	// If no specific routes matched, use default ChatID
	if len(targetChatIDs) == 0 {
		if s.cfg.ChatID != 0 {
			targetChatIDs[s.cfg.ChatID] = true
		} else {
			// Try query param
			q := r.URL.Query().Get("chat_id")
			if q != "" {
				var id int64
				fmt.Sscanf(q, "%d", &id)
				targetChatIDs[id] = true
			}
		}
	}

	if len(targetChatIDs) == 0 {
		log.Printf("⚠️ No target ChatID found for alert")
		http.Error(w, "No destination chat found", http.StatusBadRequest)
		return
	}

	log.Printf("🎯 Sending alert to ChatIDs: %v", targetChatIDs)

	// Process and send to all targets
	for chatID := range targetChatIDs {
		if err := s.bot.SendAlert(chatID, alertPayload); err != nil {
			log.Printf("❌ Failed to send alert to ChatID %d: %v", chatID, err)
			alertsSendFailed.WithLabelValues(alertPayload.Status, fmt.Sprintf("%d", chatID)).Inc()
		} else {
			alertsSent.WithLabelValues(alertPayload.Status, fmt.Sprintf("%d", chatID)).Inc()
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Alert handled"))
}
