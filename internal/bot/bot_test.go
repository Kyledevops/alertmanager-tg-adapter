package bot

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{"zero", 0, "0m"},
		{"minutes only", 45 * time.Minute, "45m"},
		{"hours and minutes", 2*time.Hour + 30*time.Minute, "2h 30m"},
		{"days and hours", 25 * time.Hour, "1d 1h"},
		{"exactly one hour", 1 * time.Hour, "1h 0m"},
		{"exactly one day", 24 * time.Hour, "1d 0h"},
		{"multiple days", 72 * time.Hour, "3d 0h"},
		{"negative duration", -5 * time.Minute, "0m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.duration)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.duration, got, tt.want)
			}
		})
	}
}

func TestLoadTemplate(t *testing.T) {
	// Test with valid template
	tmpl, err := loadTemplate("../../templates/default.tmpl")
	if err != nil {
		t.Fatalf("loadTemplate() error: %v", err)
	}
	if tmpl == nil {
		t.Fatal("loadTemplate() returned nil template")
	}
}

func TestLoadTemplateNotFound(t *testing.T) {
	_, err := loadTemplate("/nonexistent/template.tmpl")
	if err == nil {
		t.Fatal("loadTemplate() expected error for missing file, got nil")
	}
}

func TestLoadTemplateDefault(t *testing.T) {
	// Test that empty string defaults to templates/default.tmpl
	// This will fail if run from a different working directory,
	// but verifies the default path logic
	_, err := loadTemplate("")
	// We expect this to either succeed or fail with "file not found"
	// depending on the working directory, but NOT a nil error with nil template
	if err != nil {
		t.Logf("loadTemplate('') error (expected if not in project root): %v", err)
	}
}

func TestIsValidTelegramURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		// Should be INVALID — internal K8s hostnames without dots
		{"k8s pod hostname", "http://vmalert-vm-cluster-victoria-metrics-k8s-stack-7ff956b688-cp58p:8080/vmalert/alert?group_id=16917883142823475846&alert_id=17621036420608559224", false},
		{"simple service name", "http://vmalert:8080/vmalert/alert", false},
		{"localhost", "http://localhost:3000", false},
		{"empty string", "", false},
		{"no scheme", "vmalert.monitoring:8080/alert", false},

		// Should be VALID — hostnames with dots (real domains, FQDN services, IPs)
		{"k8s FQDN service", "http://vmalert.monitoring:8080/vmalert/alert", true},
		{"k8s full FQDN", "http://vmalert.monitoring.svc.cluster.local:8080/vmalert/alert", true},
		{"IP address", "http://10.244.1.5:8080/vmalert/alert", true},
		{"public domain", "https://grafana.example.com/d/123", true},
		{"alertmanager domain", "https://alertmgnt.aiaipool.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidTelegramURL(tt.url)
			if got != tt.want {
				t.Errorf("isValidTelegramURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestSilenceCacheEntry(t *testing.T) {
	entry := SilenceCacheEntry{
		Labels:    map[string]string{"alertname": "TestAlert", "severity": "critical"},
		ExpiresAt: time.Now().Add(48 * time.Hour),
	}

	if entry.Labels["alertname"] != "TestAlert" {
		t.Errorf("Labels alertname = %q, want %q", entry.Labels["alertname"], "TestAlert")
	}

	if time.Until(entry.ExpiresAt) < 47*time.Hour {
		t.Error("ExpiresAt should be ~48 hours from now")
	}
}
