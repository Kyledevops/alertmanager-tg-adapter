package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAlertManagerWebhookDecode(t *testing.T) {
	payload := `{
		"version": "4",
		"groupKey": "{}:{alertname=\"HighMemoryUsage\"}",
		"status": "firing",
		"receiver": "telegram-adapter",
		"groupLabels": {"alertname": "HighMemoryUsage"},
		"commonLabels": {
			"alertname": "HighMemoryUsage",
			"severity": "critical",
			"instance": "server-01"
		},
		"commonAnnotations": {
			"summary": "High memory usage detected",
			"description": "Memory usage is above 90% for more than 5 minutes."
		},
		"externalURL": "https://alertmanager.example.com",
		"alerts": [
			{
				"status": "firing",
				"labels": {
					"alertname": "HighMemoryUsage",
					"severity": "critical",
					"instance": "server-01",
					"job": "node_exporter"
				},
				"annotations": {
					"summary": "High memory usage on server-01",
					"description": "Memory usage is at 92%.",
					"runbook_url": "https://wiki.example.com/runbook/high-memory"
				},
				"startsAt": "2023-10-25T10:00:00Z",
				"endsAt": "0001-01-01T00:00:00Z",
				"generatorURL": "https://prometheus.example.com/graph",
				"fingerprint": "abc123"
			}
		]
	}`

	var webhook AlertManagerWebhook
	err := json.Unmarshal([]byte(payload), &webhook)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if webhook.Status != "firing" {
		t.Errorf("Status = %q, want %q", webhook.Status, "firing")
	}
	if webhook.Receiver != "telegram-adapter" {
		t.Errorf("Receiver = %q, want %q", webhook.Receiver, "telegram-adapter")
	}
	if len(webhook.Alerts) != 1 {
		t.Fatalf("Alerts count = %d, want 1", len(webhook.Alerts))
	}
	if webhook.Alerts[0].Labels["alertname"] != "HighMemoryUsage" {
		t.Errorf("Alert alertname = %q, want %q", webhook.Alerts[0].Labels["alertname"], "HighMemoryUsage")
	}
	if webhook.Alerts[0].Annotations["runbook_url"] != "https://wiki.example.com/runbook/high-memory" {
		t.Errorf("Alert runbook_url mismatch")
	}
	if webhook.Alerts[0].Fingerprint != "abc123" {
		t.Errorf("Fingerprint = %q, want %q", webhook.Alerts[0].Fingerprint, "abc123")
	}
	if webhook.ExternalURL != "https://alertmanager.example.com" {
		t.Errorf("ExternalURL = %q, want %q", webhook.ExternalURL, "https://alertmanager.example.com")
	}
	if webhook.CommonLabels["severity"] != "critical" {
		t.Errorf("CommonLabels severity = %q, want %q", webhook.CommonLabels["severity"], "critical")
	}
}

func TestAlertmanagerSilenceDecode(t *testing.T) {
	payload := `{
		"id": "silence-123",
		"matchers": [
			{"name": "alertname", "value": "HighCPU", "isRegex": false}
		],
		"startsAt": "2025-01-01T00:00:00Z",
		"endsAt": "2025-01-01T04:00:00Z",
		"createdBy": "@testuser",
		"comment": "Test silence",
		"status": {"state": "active"},
		"updatedAt": "2025-01-01T00:00:00Z"
	}`

	var silence AlertmanagerSilence
	err := json.Unmarshal([]byte(payload), &silence)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if silence.ID != "silence-123" {
		t.Errorf("ID = %q, want %q", silence.ID, "silence-123")
	}
	if len(silence.Matchers) != 1 {
		t.Fatalf("Matchers count = %d, want 1", len(silence.Matchers))
	}
	if silence.Matchers[0].Name != "alertname" {
		t.Errorf("Matcher name = %q, want %q", silence.Matchers[0].Name, "alertname")
	}
	if silence.Status.State != "active" {
		t.Errorf("Status.State = %q, want %q", silence.Status.State, "active")
	}
	if silence.CreatedBy != "@testuser" {
		t.Errorf("CreatedBy = %q, want %q", silence.CreatedBy, "@testuser")
	}
}

func TestAlertmanagerAlertDecode(t *testing.T) {
	payload := `{
		"annotations": {"summary": "test alert"},
		"endsAt": "0001-01-01T00:00:00Z",
		"fingerprint": "fp123",
		"generatorURL": "https://prometheus.example.com/graph",
		"labels": {"alertname": "TestAlert", "severity": "warning"},
		"receivers": [{"name": "telegram"}],
		"startsAt": "2025-01-01T10:00:00Z",
		"status": {
			"inhibitedBy": [],
			"silencedBy": [],
			"state": "active"
		},
		"updatedAt": "2025-01-01T10:00:00Z"
	}`

	var alert AlertmanagerAlert
	err := json.Unmarshal([]byte(payload), &alert)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if alert.Labels["alertname"] != "TestAlert" {
		t.Errorf("alertname = %q, want %q", alert.Labels["alertname"], "TestAlert")
	}
	if alert.Status.State != "active" {
		t.Errorf("Status.State = %q, want %q", alert.Status.State, "active")
	}
	if len(alert.Receivers) != 1 || alert.Receivers[0].Name != "telegram" {
		t.Errorf("Receivers mismatch")
	}
}

func TestMessageRecord(t *testing.T) {
	record := MessageRecord{
		ChatID:    -100123456,
		MessageID: 42,
		SentAt:    time.Now(),
	}

	if record.ChatID != -100123456 {
		t.Errorf("ChatID = %d, want %d", record.ChatID, -100123456)
	}
	if record.MessageID != 42 {
		t.Errorf("MessageID = %d, want %d", record.MessageID, 42)
	}
}
