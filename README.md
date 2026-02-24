# Alertmanager Telegram Adapter

一個功能豐富的 Webhook 伺服器，將 Prometheus Alertmanager 的告警轉換為美觀、可互動的 Telegram 通知。

## ✨ 功能

### 📬 告警通知
- **美觀格式化**：使用 Emoji、粗體、HTML 結構化顯示告警內容
- **自定義模板**：支援 Go Template 自定義告警訊息格式
- **Label-based 路由**：根據告警標籤（如 `team`, `severity`）自動路由至不同的 Telegram 群組
- **智慧 Resolved 通知**：告警恢復時自動**編輯原始訊息**（🔥→✅），而非發送新訊息，保持聊天室整潔

### 🔘 互動按鈕
| 按鈕 | 功能 |
|---|---|
| **🔕 Silence** | 跳轉 Alertmanager Silence 表單，**自動帶入所有告警標籤**作為匹配條件 |
| **📲 Grafana** | 跳轉 Grafana Dashboard（優先使用 `dashboard_url` 註解，否則使用 Prometheus Graph） |
| **📘 Runbook** | 跳轉維運手冊連結 |
| **🔕 1h / 4h / 24h** | 一鍵在 Alertmanager 建立 Silence（直接呼叫 API，無需跳轉） |
| **👀 Acknowledge** | 認領告警，Bot 回覆認領者並更新按鈕狀態為 `✅ Ack'd by @user` |

### 🤖 Bot 指令
| 指令 | 說明 | 範例 |
|---|---|---|
| `/status` | 查看目前所有 firing 告警（按 alertname 分組，顯示 severity 和持續時間） | `/status` |
| `/silences` | 列出所有活躍的 Silence，附 **❌ 取消** 按鈕可直接移除 | `/silences` |
| `/silence` | 建立自訂時長的 Silence，支援天數格式 | `/silence 2h alertname=HighCPU` |
| `/help` | 顯示幫助訊息 | `/help` |

### 🔧 系統功能
- **Prometheus Metrics**：暴露 `/metrics` 端點，可監控收到/發送/失敗的告警數量
- **Graceful Shutdown**：支援 `SIGTERM`/`SIGINT` 優雅關閉，給予 15 秒完成處理中請求
- **Health Check**：`/health` 端點供 K8s / Docker 健康檢查
- **Silence Cache TTL**：自動清理過期的 Silence 上下文（48h TTL）
- **Message Cache**：追蹤 firing 訊息用於 edit-on-resolve（48h TTL 自動清理）

---

## 🚀 快速開始

### 1. 配置 (`config.yml`)

```yaml
telegram_token: "YOUR_TELEGRAM_BOT_TOKEN"
chat_id: -100123456789        # 預設聊天室 ID
listen_addr: ":9087"          # 監聽地址
alertmanager_internal_url: "http://alertmanager:9093" # Alertmanager 內網地址 (API)
alertmanager_external_url: "https://alertmanager.example.com" # Alertmanager 外網地址 (按鈕跳轉)


# 路由設定 (可選)
# 根據 CommonLabels 匹配，將告警路由至不同群組
# 格式: "label_name=label_value": chat_id
routes:
  "team=frontend": -100111111111
  "team=backend":  -100222222222
  "severity=critical": -100333333333
```

也可使用環境變數覆蓋：

| 環境變數 | 說明 |
|---|---|
| `TELEGRAM_TOKEN` | Telegram Bot Token（必填） |
| `CHAT_ID` | 預設聊天室 ID |
| `LISTEN_ADDR` | 監聽地址，預設 `:9087` |
| `ALERTMANAGER_INTERNAL_URL` | Alertmanager 內網地址，預設 `http://localhost:9093` |
| `ALERTMANAGER_EXTERNAL_URL` | Alertmanager 外網地址，用於按鈕連結 (可選) |
| `TEMPLATE_FILE` | 模板檔案路徑 |
| `CONFIG_FILE` | 配置檔路徑，預設 `config.yml` |
| `ROUTES` | 路由設定，格式：`severity=critical:-100123,team=backend:-100456` |

### 2. 使用 Docker Compose 執行

```bash
docker-compose up -d
```

### 3. 本地執行

```bash
# 使用 Makefile 編譯並推送至 Harbor
make docker-build
make docker-push
```

### 4. Kubernetes 部署

```bash
# 套用 K8s 設定 (Deployment, Service, ConfigMap)
kubectl apply -f k8s.yaml
```

### 4. 配置 Alertmanager

在 `alertmanager.yml` 中新增 webhook receiver：

```yaml
receivers:
  - name: 'telegram-adapter'
    webhook_configs:
      - url: 'http://tg-adapter:9087/webhook'
        send_resolved: true
```

---

## 📝 自定義模板

模板檔案位於 `templates/default.tmpl`，使用 Go `text/template` 語法。

### 可用變數

| 變數 | 說明 |
|---|---|
| `.Status` | 告警狀態 (`firing` / `resolved`) |
| `.Alerts` | 告警列表 |
| `.Alerts[].Labels` | 標籤 Map |
| `.Alerts[].Annotations` | 註解 Map |
| `.Alerts[].StartsAt` | 開始時間 |
| `.Alerts[].EndsAt` | 結束時間 |
| `.Alerts[].GeneratorURL` | Prometheus 圖表連結 |
| `.CommonLabels` | 共同標籤 Map |
| `.CommonAnnotations` | 共同註解 Map |
| `.ExternalURL` | Alertmanager 外部 URL |
| `.GroupKey` | 群組 Key |

### 可用函數

| 函數 | 說明 | 範例 |
|---|---|---|
| `toUpper` | 轉大寫 | `{{ .Status \| toUpper }}` |
| `htmlEscape` | HTML 跳脫 | `{{ .Labels.instance \| htmlEscape }}` |
| `timeFormat` | 時間格式化 | `{{ timeFormat "15:04" .StartsAt }}` |

---

## 📋 Prometheus Rule 配置範例

為了讓 Bot 顯示完整資訊和正確的按鈕，請配置對應的 Labels 和 Annotations：

```yaml
groups:
  - name: heavy-load
    rules:
      - alert: HighCpuLoad
        expr: 100 - (avg by(instance) (rate(node_cpu_seconds_total{mode="idle"}[2m])) * 100) > 80
        for: 5m
        labels:
          severity: "critical"
        annotations:
          summary: "Host {{ $labels.instance }} CPU load is high"
          description: "CPU load is > 80% for more than 5 minutes.\nCurrent value: {{ $value | printf \"%.2f\" }}%"
          runbook_url: "https://wiki.example.com/runbooks/high-cpu-load"
          dashboard_url: "https://grafana.yourcompany.com/d/node-exporter?var-instance={{ $labels.instance }}"
```

### 欄位對應關係

| Bot 功能 | 對應 Prometheus 欄位 | 說明 |
|---|---|---|
| 🚨 **標題** | `labels.alertname` | 告警規則名稱 |
| 🎚 **Severity** | `labels.severity` | `critical` / `warning` / `info` |
| 🖥 **Instance** | `labels.instance` | 通常由 Prometheus 自動加上 |
| 📝 **Summary** | `annotations.summary` | 簡短摘要 |
| ℹ️ **Description** | `annotations.description` | 詳細說明 |
| 📘 **Runbook 按鈕** | `annotations.runbook_url` | 維運手冊連結 |
| 📲 **Grafana 按鈕** | `annotations.dashboard_url` | 自定義 Dashboard 連結；未設定時使用 `generatorURL` |
| 🔕 **Silence 按鈕** | `externalURL` + `commonLabels` | 自動產生帶標籤 filter 的 Silence 表單 URL |
| 🔕 **1h/4h/24h** | `commonLabels` | 直接呼叫 Alertmanager API 建立 Silence |
| 👀 **Acknowledge** | (自動生成) | `firing` 狀態自動出現，點擊後顯示認領者 |

---

## 🔌 API 端點

| 端點 | 方法 | 說明 |
|---|---|---|
| `/webhook` | `POST` | 接收 Alertmanager Webhook |
| `/health` | `GET` | 健康檢查，回傳 `OK` |
| `/metrics` | `GET` | Prometheus Metrics |

### Metrics 指標

| 指標名稱 | 類型 | Labels | 說明 |
|---|---|---|---|
| `alert_telegram_adapter_received_total` | Counter | `status` | 接收的告警總數 |
| `alert_telegram_adapter_sent_total` | Counter | `status`, `chat_id` | 成功發送到 Telegram 的告警數 |
| `alert_telegram_adapter_send_failed_total` | Counter | `status`, `chat_id` | 發送失敗的告警數 |
| `alert_telegram_adapter_request_duration_seconds` | Histogram | `path` | HTTP 請求處理時間 |

---

## 🛠 Makefile 指令

```bash
make help               # 顯示所有可用指令
make run                 # 本地編譯並執行
make build               # 僅編譯
make docker-build        # 建立 Docker Image
make docker-run          # 使用 Docker Compose 啟動
make docker-stop         # 停止 Docker Compose
make test-alert          # 發送測試告警 (firing)
make test-alert-resolved # 發送測試告警 (resolved)
```

---

## 🏗 專案結構

```
alertmanager-tg-adapter/
├── main.go                          # 入口：初始化、Graceful Shutdown
├── config.yml                       # 配置檔
├── Dockerfile                       # 多階段建置 (test → build → runtime)
├── docker-compose.yml               # Docker Compose 部署
├── Makefile                         # 開發指令
├── templates/
│   └── default.tmpl                 # 告警訊息模板
├── test/
│   └── payload.json                 # 測試用 Webhook Payload
└── internal/
    ├── config/
    │   ├── config.go                # 配置載入 (YAML + 環境變數)
    │   └── config_test.go           # 配置單元測試
    ├── model/
    │   ├── alert.go                 # 資料結構 (Webhook, Alert, Silence, etc.)
    │   └── alert_test.go            # Model 單元測試
    ├── bot/
    │   ├── bot.go                   # Telegram Bot 核心邏輯
    │   └── bot_test.go              # Bot 單元測試
    └── server/
        ├── server.go                # HTTP Server、路由、Metrics
        └── server_test.go           # Server 單元測試
```

---

## 📄 License

MIT
