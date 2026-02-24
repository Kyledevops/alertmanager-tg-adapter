package model

import "time"

// Alertmanager Webhook payload structure
// Reference: https://prometheus.io/docs/alerting/latest/configuration/#webhook_config

type AlertManagerWebhook struct {
	Receiver          string            `json:"receiver"`
	Status            string            `json:"status"` // firing, resolved
	Alerts            []Alert           `json:"alerts"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
}

type Alert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
	Value        string            `json:"value"`
}

// Alertmanager API v2 response types

type AlertmanagerAlert struct {
	Annotations  map[string]string `json:"annotations"`
	EndsAt       time.Time         `json:"endsAt"`
	Fingerprint  string            `json:"fingerprint"`
	GeneratorURL string            `json:"generatorURL"`
	Labels       map[string]string `json:"labels"`
	Receivers    []AlertReceiver   `json:"receivers"`
	StartsAt     time.Time         `json:"startsAt"`
	Status       AlertStatus       `json:"status"`
	UpdatedAt    time.Time         `json:"updatedAt"`
}

type AlertReceiver struct {
	Name string `json:"name"`
}

type AlertStatus struct {
	InhibitedBy []string `json:"inhibitedBy"`
	SilencedBy  []string `json:"silencedBy"`
	State       string   `json:"state"` // active, suppressed, unprocessed
}

type AlertmanagerSilence struct {
	ID        string           `json:"id"`
	Matchers  []SilenceMatcher `json:"matchers"`
	StartsAt  time.Time        `json:"startsAt"`
	EndsAt    time.Time        `json:"endsAt"`
	CreatedBy string           `json:"createdBy"`
	Comment   string           `json:"comment"`
	Status    SilenceStatus    `json:"status"`
	UpdatedAt time.Time        `json:"updatedAt"`
}

type SilenceMatcher struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"isRegex"`
	IsEqual *bool  `json:"isEqual,omitempty"`
}

type SilenceStatus struct {
	State string `json:"state"` // active, pending, expired
}

// MessageRecord tracks sent messages for edit-on-resolve feature
type MessageRecord struct {
	ChatID    int64
	MessageID int
	SentAt    time.Time
}
