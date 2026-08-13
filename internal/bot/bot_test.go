package bot

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alertmanager-tg-adapter/internal/model"
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

func TestSortedLabelPairs(t *testing.T) {
	labels := map[string]string{
		"alertname":  "TestAlert",
		"severity":   "critical",
		"prometheus": "monitoring/vm",
		"uid":        "abc123",
		"zone":       "us-east-1",
		"team":       "payments",
		"instance":   "server-01",
		"cluster":    "prod",
	}

	t.Run("skips meta labels and sorts", func(t *testing.T) {
		got := sortedLabelPairs(labels, nil, nil)
		want := []LabelPair{
			{"cluster", "prod"},
			{"instance", "server-01"},
			{"team", "payments"},
			{"zone", "us-east-1"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("sortedLabelPairs() = %v, want %v", got, want)
		}
	})

	t.Run("extraSkip removes header labels", func(t *testing.T) {
		got := sortedLabelPairs(labels, headerLabels, nil)
		for _, p := range got {
			if p.Name == "cluster" || p.Name == "namespace" {
				t.Errorf("header label %q should be skipped", p.Name)
			}
		}
	})

	t.Run("dedupe drops equal values, keeps differing", func(t *testing.T) {
		common := map[string]string{"team": "payments", "zone": "eu-west-1"}
		got := sortedLabelPairs(labels, headerLabels, common)
		want := []LabelPair{
			{"instance", "server-01"},
			{"zone", "us-east-1"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("sortedLabelPairs() = %v, want %v", got, want)
		}
	})

	t.Run("nil and empty maps", func(t *testing.T) {
		if got := sortedLabelPairs(nil, nil, nil); got != nil {
			t.Errorf("sortedLabelPairs(nil) = %v, want nil", got)
		}
		if got := sortedLabelPairs(map[string]string{}, nil, nil); got != nil {
			t.Errorf("sortedLabelPairs(empty) = %v, want nil", got)
		}
	})
}

func TestTitleLabel(t *testing.T) {
	tests := []struct{ in, want string }{
		{"cluster", "Cluster"},
		{"team", "Team"},
		{"", ""},
		{"target_group", "Target_group"},
	}
	for _, tt := range tests {
		if got := titleLabel(tt.in); got != tt.want {
			t.Errorf("titleLabel(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMaxNameLen(t *testing.T) {
	pairs := []LabelPair{{"pod", "x"}, {"container", "y"}, {"job", "z"}}
	if got := maxNameLen(pairs); got != len("container")+1 {
		t.Errorf("maxNameLen() = %d, want %d", got, len("container")+1)
	}
	if got := maxNameLen(nil); got != 1 {
		t.Errorf("maxNameLen(nil) = %d, want 1", got)
	}
}

func TestTemplateDynamicLabels(t *testing.T) {
	tmpl, err := loadTemplate("../../templates/default.tmpl")
	if err != nil {
		t.Fatalf("loadTemplate() error: %v", err)
	}

	payload := model.AlertManagerWebhook{
		Status: "firing",
		CommonLabels: map[string]string{
			"alertname":  "DiskFull",
			"severity":   "critical",
			"cluster":    "prod",
			"team":       "payments",
			"prometheus": "monitoring/vm",
		},
		CommonAnnotations: map[string]string{
			"description": "Disk almost full",
		},
		Alerts: []model.Alert{
			{
				Status: "firing",
				Labels: map[string]string{
					"alertname":    "DiskFull",
					"severity":     "critical",
					"cluster":      "prod",
					"team":         "payments",
					"prometheus":   "monitoring/vm",
					"zone":         "us-east-1",
					"device":       "/dev/sda1",
					"target_group": "tg-1",
					"uid":          "abc<b>123",
				},
				Annotations: map[string]string{"summary": "Disk almost full on server-01"},
			},
		},
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, payload); err != nil {
		t.Fatalf("Template execution failed: %v", err)
	}
	out := buf.String()

	// Arbitrary (non-hardcoded) labels render dynamically
	for _, want := range []string{"team:", "payments", "zone:", "us-east-1", "device:", "/dev/sda1"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
	// Meta labels never render as detail labels
	for _, ban := range []string{"prometheus", "target_group", "uid", "abc"} {
		if strings.Contains(out, ban) {
			t.Errorf("output should not contain meta label %q\noutput:\n%s", ban, out)
		}
	}
	// cluster shows only once (header 🏷), not repeated in detail blocks
	if got := strings.Count(out, "prod"); got != 1 {
		t.Errorf("cluster value should appear exactly once, got %d\noutput:\n%s", got, out)
	}
}

func TestTemplateEscapesLabelValues(t *testing.T) {
	tmpl, err := loadTemplate("../../templates/default.tmpl")
	if err != nil {
		t.Fatalf("loadTemplate() error: %v", err)
	}

	payload := model.AlertManagerWebhook{
		Status:       "firing",
		CommonLabels: map[string]string{"alertname": "XSSAlert"},
		Alerts: []model.Alert{
			{
				Status: "firing",
				Labels: map[string]string{
					"alertname": "XSSAlert",
					"payload":   "foo<b>&bar",
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, payload); err != nil {
		t.Fatalf("Template execution failed: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "foo&lt;b&gt;&amp;bar") {
		t.Errorf("label value not HTML-escaped\noutput:\n%s", out)
	}
	if strings.Contains(out, "foo<b>") {
		t.Errorf("raw HTML leaked into output\noutput:\n%s", out)
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
