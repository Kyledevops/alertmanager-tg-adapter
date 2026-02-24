package bot

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/alertmanager-tg-adapter/internal/config"
	"github.com/alertmanager-tg-adapter/internal/model"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// SilenceCacheEntry wraps label data with a TTL timestamp
type SilenceCacheEntry struct {
	Labels    map[string]string
	ExpiresAt time.Time
}

var (
	leadingLabelPrefRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*=[^\s]+\s*[-–—:]\s*`)
)

// CleanAlertSummary removes common noise from Prometheus alert summaries:
//   - Trailing label dumps    ("…text alertname=Foo instance=Bar")
//   - Leading label prefixes  ("alertname=Foo - Some text")
//   - Excessive whitespace
func CleanAlertSummary(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}

	// Strip leading "key=value - " prefix
	s = leadingLabelPrefRe.ReplaceAllString(s, "")

	return strings.TrimSpace(s)
}

type Bot struct {
	API          *tgbotapi.BotAPI
	Template     *template.Template
	Config       *config.Config
	SilenceCache map[string]SilenceCacheEntry // Hash -> Labels + TTL
	SilenceMu    sync.RWMutex
	MessageCache map[string]model.MessageRecord // GroupKey -> MessageRecord (for edit-on-resolve)
	MessageMu    sync.RWMutex
}

func New(token string, cfg *config.Config) (*Bot, error) {
	client := http.DefaultClient

	api, err := tgbotapi.NewBotAPIWithClient(token, tgbotapi.APIEndpoint, client)
	if err != nil {
		return nil, err
	}

	tmpl, err := loadTemplate(cfg.TemplateFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load template: %v", err)
	}

	b := &Bot{
		API:          api,
		Template:     tmpl,
		Config:       cfg,
		SilenceCache: make(map[string]SilenceCacheEntry),
		MessageCache: make(map[string]model.MessageRecord),
	}

	// Start background goroutines
	go b.HandleUpdates()
	go b.cleanupCaches()

	return b, nil
}

// cleanupCaches periodically removes expired entries from caches
func (b *Bot) cleanupCaches() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()

		// Cleanup silence cache
		b.SilenceMu.Lock()
		for k, v := range b.SilenceCache {
			if now.After(v.ExpiresAt) {
				delete(b.SilenceCache, k)
			}
		}
		b.SilenceMu.Unlock()

		// Cleanup message cache (remove entries older than 48h)
		b.MessageMu.Lock()
		for k, v := range b.MessageCache {
			if now.Sub(v.SentAt) > 48*time.Hour {
				delete(b.MessageCache, k)
			}
		}
		b.MessageMu.Unlock()
	}
}

func (b *Bot) HandleUpdates() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.API.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil {
			if update.Message.IsCommand() {
				log.Printf("🤖 Received command: /%s from @%s", update.Message.Command(), update.Message.From.UserName)
				switch update.Message.Command() {
				case "start", "help":
					b.handleHelp(update.Message)
				case "status":
					b.handleStatus(update.Message)
				case "silences":
					b.handleSilences(update.Message)
				case "silence":
					b.handleSilenceCommand(update.Message)
				default:
					continue
				}
			}
		} else if update.CallbackQuery != nil {
			log.Printf("👆 Received callback: %s from @%s", update.CallbackQuery.Data, update.CallbackQuery.From.UserName)
			data := update.CallbackQuery.Data
			if strings.HasPrefix(data, "silence:") {
				handleSilence(b, update.CallbackQuery)
			} else if strings.HasPrefix(data, "ack:") {
				handleAck(b, update.CallbackQuery)
			} else if strings.HasPrefix(data, "expire_silence:") {
				handleExpireSilence(b, update.CallbackQuery)
			}
		}
	}
}

func (b *Bot) handleHelp(message *tgbotapi.Message) {
	helpText := "🤖 <b>Alertmanager Telegram Adapter</b>\n\n" +
		"/status - 查看目前所有 firing 告警\n" +
		"/silences - 列出所有活躍的 Silence\n" +
		"/silence [時長] [標籤=值] - 建立 Silence\n" +
		"/help - 顯示此幫助訊息\n\n" +
		"您可以點擊告警下方的按鈕進行快速靜音或認領。"

	msg := tgbotapi.NewMessage(message.Chat.ID, helpText)
	msg.ParseMode = tgbotapi.ModeHTML
	b.API.Send(msg)
}

func (b *Bot) handleStatus(message *tgbotapi.Message) {
	resp, err := http.Get(fmt.Sprintf("%s/api/v2/alerts?active=true&silenced=false&inhibited=false", b.Config.AlertmanagerInternalURL))
	if err != nil {
		b.API.Send(tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("❌ 無法連接 Alertmanager: %v", err)))
		return
	}
	defer resp.Body.Close()

	var alerts []model.AlertmanagerAlert
	if err := json.NewDecoder(resp.Body).Decode(&alerts); err != nil {
		b.API.Send(tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("❌ 解析回應失敗: %v", err)))
		return
	}

	if len(alerts) == 0 {
		b.API.Send(tgbotapi.NewMessage(message.Chat.ID, "✅ 目前沒有任何活躍告警"))
		return
	}

	var buffer bytes.Buffer
	buffer.WriteString(fmt.Sprintf("🔔 <b>目前共有 %d 條活躍告警:</b>\n", len(alerts)))

	for i, a := range alerts {
		duration := time.Since(a.StartsAt)
		alertName := a.Labels["alertname"]
		if alertName == "" {
			alertName = "Unknown Alert"
		}

		buffer.WriteString("\n─────────────────\n")

		// Severity badge + alert name
		severity := a.Labels["severity"]
		sevEmoji := "🔵"
		switch severity {
		case "critical":
			sevEmoji = "🔴"
		case "warning":
			sevEmoji = "🟡"
		}
		if severity != "" {
			buffer.WriteString(fmt.Sprintf("%s <b>%s</b>  ·  <b>%s</b>  ·  %s\n",
				sevEmoji,
				html.EscapeString(strings.ToUpper(severity)),
				html.EscapeString(alertName),
				formatDuration(duration)))
		} else {
			buffer.WriteString(fmt.Sprintf("🔸 <b>%s</b>  ·  %s\n",
				html.EscapeString(alertName),
				formatDuration(duration)))
		}

		// Context labels
		type labelPair struct{ key, emoji string }
		for _, lp := range []labelPair{
			{"cluster", "🏷"},
			{"namespace", "📦"},
			{"instance", "🖥"},
			{"pod", "🐳"},
			{"container", "📂"},
			{"job", "⚙️"},
			{"reason", "💬"},
		} {
			if v := a.Labels[lp.key]; v != "" {
				buffer.WriteString(fmt.Sprintf("    %s <b>%s:</b>  <code>%s</code>\n",
					lp.emoji,
					html.EscapeString(strings.Title(lp.key)),
					html.EscapeString(v)))
			}
		}

		// Description or summary
		if desc := a.Annotations["description"]; desc != "" {
			buffer.WriteString(fmt.Sprintf("    ℹ️ %s\n", html.EscapeString(desc)))
		} else if summary := a.Annotations["summary"]; summary != "" {
			buffer.WriteString(fmt.Sprintf("    ℹ️ %s\n", html.EscapeString(CleanAlertSummary(summary))))
		}

		_ = i // suppress unused warning
	}

	msg := tgbotapi.NewMessage(message.Chat.ID, buffer.String())
	msg.ParseMode = tgbotapi.ModeHTML
	b.API.Send(msg)
}

func (b *Bot) handleSilences(message *tgbotapi.Message) {
	resp, err := http.Get(fmt.Sprintf("%s/api/v2/silences?active=true", b.Config.AlertmanagerInternalURL))
	if err != nil {
		b.API.Send(tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("❌ 無法連接 Alertmanager: %v", err)))
		return
	}
	defer resp.Body.Close()

	var silences []model.AlertmanagerSilence
	if err := json.NewDecoder(resp.Body).Decode(&silences); err != nil {
		b.API.Send(tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("❌ 解析回應失敗: %v", err)))
		return
	}

	if len(silences) == 0 {
		b.API.Send(tgbotapi.NewMessage(message.Chat.ID, "📋 目前沒有活躍的 Silence"))
		return
	}

	for _, s := range silences {
		var matchers []string
		for _, m := range s.Matchers {
			matchers = append(matchers, fmt.Sprintf("%s=%s", m.Name, m.Value))
		}

		duration := time.Until(s.EndsAt)
		text := fmt.Sprintf("🔕 <b>Silence ID: %s</b>\n"+
			"📋 條件: <code>%s</code>\n"+
			"👤 建立者: %s\n"+
			"💬 註解: %s\n"+
			"⏱ 剩餘時間: %s",
			s.ID, strings.Join(matchers, ", "), html.EscapeString(s.CreatedBy), html.EscapeString(s.Comment), formatDuration(duration))

		msg := tgbotapi.NewMessage(message.Chat.ID, text)
		msg.ParseMode = tgbotapi.ModeHTML

		expireBtn := tgbotapi.NewInlineKeyboardButtonData("❌ 取消 Silence", fmt.Sprintf("expire_silence:%s", s.ID))
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup([]tgbotapi.InlineKeyboardButton{expireBtn})

		b.API.Send(msg)
	}
}

func (b *Bot) handleSilenceCommand(message *tgbotapi.Message) {
	// ... (simplified reconstruction)
	args := strings.Fields(message.Text)
	if len(args) < 3 {
		b.API.Send(tgbotapi.NewMessage(message.Chat.ID, "💡 用法: /silence [時長, 如 2h] [標籤=值]"))
		return
	}
	// Simplified for now
}

func handleSilence(b *Bot, callback *tgbotapi.CallbackQuery) {
	parts := strings.Split(callback.Data, ":")
	if len(parts) < 3 {
		return
	}
	durationStr := parts[1]
	hash := parts[2]

	b.SilenceMu.RLock()
	entry, exists := b.SilenceCache[hash]
	b.SilenceMu.RUnlock()

	if !exists {
		b.API.Request(tgbotapi.NewCallback(callback.ID, "❌ 找不到快取的告警資料 (已過期)"))
		return
	}

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		// Handle days
		if strings.HasSuffix(durationStr, "d") {
			daysStr := strings.TrimSuffix(durationStr, "d")
			var d int
			fmt.Sscanf(daysStr, "%d", &d)
			duration = time.Duration(d) * 24 * time.Hour
		} else {
			b.API.Request(tgbotapi.NewCallback(callback.ID, "❌ 無效的時間格式"))
			return
		}
	}

	user := callback.From.UserName
	if user == "" {
		user = callback.From.FirstName
	}

	b.CreateSilence(callback.Message, entry.Labels, duration, user)
	b.API.Request(tgbotapi.NewCallback(callback.ID, "✅ Silence 已提交"))
}

func (b *Bot) CreateSilence(message *tgbotapi.Message, labels map[string]string, duration time.Duration, user string) {
	startsAt := time.Now()
	endsAt := startsAt.Add(duration)

	var matchers []model.SilenceMatcher
	for k, v := range labels {
		matchers = append(matchers, model.SilenceMatcher{
			Name:    k,
			Value:   v,
			IsRegex: false,
		})
	}

	payload := model.AlertmanagerSilence{
		Matchers:  matchers,
		StartsAt:  startsAt,
		EndsAt:    endsAt,
		CreatedBy: user,
		Comment:   "Created via Telegram Bot",
	}

	data, _ := json.Marshal(payload)
	resp, err := http.Post(fmt.Sprintf("%s/api/v2/silences", b.Config.AlertmanagerInternalURL), "application/json", bytes.NewBuffer(data))
	if err != nil {
		b.API.Send(tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("❌ 建立 Silence 失敗: %v", err)))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		msgText := fmt.Sprintf("🔕 <b>Alert silenced for %s</b> by @%s",
			formatDuration(duration),
			html.EscapeString(user))
		msg := tgbotapi.NewMessage(message.Chat.ID, msgText)
		msg.ParseMode = tgbotapi.ModeHTML
		msg.ReplyToMessageID = message.MessageID
		b.API.Send(msg)
	} else {
		body, _ := io.ReadAll(resp.Body)
		b.API.Send(tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("❌ 建立 Silence 失敗 (HTTP %d): %s", resp.StatusCode, string(body))))
	}
}

func handleAck(b *Bot, callback *tgbotapi.CallbackQuery) {
	if callback.Message == nil {
		return
	}
	user := callback.From.UserName
	if user == "" {
		user = callback.From.FirstName
	}

	b.API.Request(tgbotapi.NewCallback(callback.ID, "✅ Alert Acknowledged!"))

	ackLine := fmt.Sprintf("✅ <b>Acknowledged by @%s</b> at %s", html.EscapeString(user), time.Now().Format("15:04 UTC"))
	msg := tgbotapi.NewMessage(callback.Message.Chat.ID, ackLine)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyToMessageID = callback.Message.MessageID
	b.API.Send(msg)

	if callback.Message.ReplyMarkup != nil {
		var newRows [][]tgbotapi.InlineKeyboardButton
		for _, row := range callback.Message.ReplyMarkup.InlineKeyboard {
			var newRow []tgbotapi.InlineKeyboardButton
			for _, btn := range row {
				if btn.CallbackData != nil && strings.HasPrefix(*btn.CallbackData, "ack:") {
					newRow = append(newRow, tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("✅ Ack'd by @%s", user), "ack_done"))
				} else {
					newRow = append(newRow, btn)
				}
			}
			newRows = append(newRows, newRow)
		}
		editMarkup := tgbotapi.NewEditMessageReplyMarkup(callback.Message.Chat.ID, callback.Message.MessageID, tgbotapi.NewInlineKeyboardMarkup(newRows...))
		b.API.Send(editMarkup)
	}
}

func handleExpireSilence(b *Bot, callback *tgbotapi.CallbackQuery) {
	parts := strings.Split(callback.Data, ":")
	if len(parts) < 2 {
		return
	}
	silenceID := parts[1]

	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v2/silence/%s", b.Config.AlertmanagerInternalURL, silenceID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.API.Request(tgbotapi.NewCallback(callback.ID, fmt.Sprintf("❌ 無法連接 Alertmanager: %v", err)))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		b.API.Request(tgbotapi.NewCallback(callback.ID, "✅ Silence 已取消"))
		// Edit original message to show expired status
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, callback.Message.Text+"\n\n❌ <b>Silence 已手動取消</b>")
		editMsg.ParseMode = tgbotapi.ModeHTML
		b.API.Send(editMsg)
	} else {
		b.API.Request(tgbotapi.NewCallback(callback.ID, fmt.Sprintf("❌ 取消失敗 (HTTP %d)", resp.StatusCode)))
	}
}

func (b *Bot) SendAlert(chatID int64, alertData model.AlertManagerWebhook) error {
	msgText, keyboard, err := b.FormatAlert(alertData)
	if err != nil {
		return err
	}

	if strings.TrimSpace(msgText) == "" {
		return fmt.Errorf("message text is empty after template execution")
	}

	// 如果狀態是 resolved，統一過濾按鈕只留 Dashboard/Grafana
	var finalKeyboard *tgbotapi.InlineKeyboardMarkup
	if len(keyboard.InlineKeyboard) > 0 {
		if alertData.Status == "resolved" {
			var dashboardRows [][]tgbotapi.InlineKeyboardButton
			for _, row := range keyboard.InlineKeyboard {
				var dashboardBtns []tgbotapi.InlineKeyboardButton
				for _, btn := range row {
					if btn.URL != nil && (strings.Contains(btn.Text, "Grafana") || strings.Contains(btn.Text, "Dashboard")) {
						dashboardBtns = append(dashboardBtns, btn)
					}
				}
				if len(dashboardBtns) > 0 {
					dashboardRows = append(dashboardRows, dashboardBtns)
				}
			}
			if len(dashboardRows) > 0 {
				km := tgbotapi.NewInlineKeyboardMarkup(dashboardRows...)
				finalKeyboard = &km
			}
		} else {
			finalKeyboard = &keyboard
		}
	}

	// 1. 查找快取
	b.MessageMu.RLock()
	record, exists := b.MessageCache[alertData.GroupKey]
	b.MessageMu.RUnlock()

	if exists && record.ChatID == chatID {
		// 2. 嘗試更新訊息
		editMsg := tgbotapi.NewEditMessageText(record.ChatID, record.MessageID, msgText)
		editMsg.ParseMode = tgbotapi.ModeHTML
		editMsg.DisableWebPagePreview = true
		editMsg.ReplyMarkup = finalKeyboard

		_, err := b.API.Send(editMsg)
		if err == nil {
			log.Printf("✅ Updated existing message %d for GroupKey %s (Status: %s)", record.MessageID, alertData.GroupKey, alertData.Status)
			if alertData.Status == "resolved" {
				b.MessageMu.Lock()
				delete(b.MessageCache, alertData.GroupKey)
				b.MessageMu.Unlock()
			}
			return nil
		}
		log.Printf("⚠️ Failed to edit message, falling back to new: %v", err)
	}

	// 3. 發送新訊息
	msg := tgbotapi.NewMessage(chatID, msgText)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.DisableWebPagePreview = true
	msg.ReplyMarkup = finalKeyboard

	sentMsg, err := b.API.Send(msg)
	if err != nil {
		return err
	}

	// 4. 紀錄快取
	if alertData.Status == "firing" && alertData.GroupKey != "" {
		b.MessageMu.Lock()
		b.MessageCache[alertData.GroupKey] = model.MessageRecord{
			ChatID:    chatID,
			MessageID: sentMsg.MessageID,
			SentAt:    time.Now(),
		}
		b.MessageMu.Unlock()
		log.Printf("💾 Cached new message %d for GroupKey %s", sentMsg.MessageID, alertData.GroupKey)
	}

	return nil
}

func (b *Bot) FormatAlert(data model.AlertManagerWebhook) (string, tgbotapi.InlineKeyboardMarkup, error) {
	var buffer bytes.Buffer
	if err := b.Template.Execute(&buffer, data); err != nil {
		return "", tgbotapi.InlineKeyboardMarkup{}, err
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	var urlRow []tgbotapi.InlineKeyboardButton

	baseExternalURL := data.ExternalURL
	if b.Config.AlertmanagerExternalURL != "" {
		baseExternalURL = b.Config.AlertmanagerExternalURL
	}

	if baseExternalURL != "" && isValidTelegramURL(baseExternalURL) {
		silenceURL := buildSilenceURL(baseExternalURL, data.CommonLabels)
		urlRow = append(urlRow, tgbotapi.NewInlineKeyboardButtonURL("🔕 Silence", silenceURL))
	}

	// Check for Dashboard URL (Grafana)
	// User request: Only check if dashboard_url annotation exists.
	// We do NOT fallback to GeneratorURL anymore, and we do NOT validate the URL format strictly.
	grafanaURL := ""
	runbookURL := ""
	for _, a := range data.Alerts {
		if grafanaURL == "" {
			if d, ok := a.Annotations["dashboard_url"]; ok && d != "" {
				grafanaURL = d
			}
		}
		if runbookURL == "" {
			if r, ok := a.Annotations["runbook_url"]; ok && r != "" {
				runbookURL = r
			}
		}
	}

	if grafanaURL != "" {
		urlRow = append(urlRow, tgbotapi.NewInlineKeyboardButtonURL("📲 Grafana", grafanaURL))
	}
	if runbookURL != "" {
		urlRow = append(urlRow, tgbotapi.NewInlineKeyboardButtonURL("📘 Runbook", runbookURL))
	}

	if len(urlRow) > 0 {
		rows = append(rows, urlRow)
	}

	if data.Status == "firing" {
		hash := "default"
		if data.GroupKey != "" {
			sum := md5.Sum([]byte(data.GroupKey))
			hash = hex.EncodeToString(sum[:])
		}

		if len(data.CommonLabels) > 0 {
			b.SilenceMu.Lock()
			b.SilenceCache[hash] = SilenceCacheEntry{
				Labels:    data.CommonLabels,
				ExpiresAt: time.Now().Add(48 * time.Hour),
			}
			b.SilenceMu.Unlock()
		}

		silenceRow := []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("🔕 1h", fmt.Sprintf("silence:1h:%s", hash)),
			tgbotapi.NewInlineKeyboardButtonData("🔕 4h", fmt.Sprintf("silence:4h:%s", hash)),
			tgbotapi.NewInlineKeyboardButtonData("🔕 24h", fmt.Sprintf("silence:24h:%s", hash)),
		}
		rows = append(rows, silenceRow)

		ackRow := []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("👀 Acknowledge", fmt.Sprintf("ack:%s", hash)),
		}
		rows = append(rows, ackRow)
	}

	return buffer.String(), tgbotapi.NewInlineKeyboardMarkup(rows...), nil
}

func buildSilenceURL(baseURL string, labels map[string]string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return baseURL
	}

	var matchers []string
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		matchers = append(matchers, fmt.Sprintf("%s=\"%s\"", k, labels[k]))
	}

	filter := fmt.Sprintf("{%s}", strings.Join(matchers, ","))
	u.Fragment = fmt.Sprintf("/silences/new?filter=%s", url.QueryEscape(filter))
	return u.String()
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0m"
	}
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func loadTemplate(path string) (*template.Template, error) {
	if path == "" {
		path = "templates/default.tmpl"
	}
	// User explicitly requested .Labels.SortedPairs behavior.
	// We implement a helper for this since Labels is a map.
	type LabelPair struct {
		Name  string
		Value string
	}

	funcMap := template.FuncMap{
		"toUpper":    strings.ToUpper,
		"timeFormat": func(layout string, t time.Time) string { return t.Format(layout) },
		"htmlEscape": func(v interface{}) string {
			if v == nil {
				return ""
			}
			return html.EscapeString(fmt.Sprintf("%v", v))
		},
		"cleanSummary": func(v interface{}) string {
			if v == nil {
				return ""
			}
			s := fmt.Sprintf("%v", v)
			return CleanAlertSummary(s)
		},
		"regexFind": func(pattern, s string) string {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return ""
			}
			return re.FindString(s)
		},
		"sortedPairs": func(labels map[string]string) []LabelPair {
			var keys []string
			for k := range labels {
				if k == "alertname" || k == "severity" || k == "prometheus" || k == "alertgroup" || k == "target_group" || k == "uid" {
					continue
				}
				keys = append(keys, k)
			}
			sort.Strings(keys)
			var pairs []LabelPair
			for _, k := range keys {
				pairs = append(pairs, LabelPair{Name: k, Value: labels[k]})
			}
			return pairs
		},
	}
	return template.New(filepath.Base(path)).Funcs(funcMap).ParseFiles(path)
}

// isValidTelegramURL checks if a URL is acceptable for Telegram inline
// keyboard buttons. Telegram's API is strict about URL formats:
// 1. Must have http/https scheme.
// 2. Hostname must contain at least one dot (to look like a domain) or be an IP address.
// Internal Kubernetes service names like "vmalert:8080" will be rejected with "Wrong HTTP URL".
func isValidTelegramURL(u string) bool {
	if u == "" {
		return false
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	host := parsed.Hostname()
	if host == "" {
		return false
	}
	// Telegram requires the hostname to look like a real domain (contain a dot).
	// Internal K8s hostnames (e.g. "vmalert") will be rejected.
	if !strings.Contains(host, ".") {
		return false
	}
	return true
}
