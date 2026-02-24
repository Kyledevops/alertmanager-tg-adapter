package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/alertmanager-tg-adapter/internal/model"
)

type LabelPair struct {
	Name  string
	Value string
}

// Replicating bot.go logic exactly
func loadReproTemplate(path string) (*template.Template, error) {
	funcMap := template.FuncMap{
		"toUpper":    strings.ToUpper,
		"timeFormat": func(layout string, t time.Time) string { return t.Format(layout) },
		"htmlEscape": func(v interface{}) string {
			if v == nil {
				return ""
			}
			return html.EscapeString(fmt.Sprintf("%v", v))
		},
		"cleanSummary": func(v interface{}) string { return fmt.Sprintf("%v", v) },
		"sortedPairs": func(labels, commonLabels map[string]string) []LabelPair {
			var keys []string
			for k := range labels {
				if k == "alertname" || k == "severity" || k == "prometheus" || k == "alertgroup" || k == "target_group" || k == "uid" {
					continue
				}
				// Skip if common label
				if v, ok := commonLabels[k]; ok && v == labels[k] {
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

func TestReproEmptyOutput(t *testing.T) {
	tmplPath := "../../templates/default.tmpl"
	tmpl, err := loadReproTemplate(tmplPath)
	if err != nil {
		t.Fatalf("Failed to load template: %v", err)
	}

	payloadFile := "../../test/payload_repro.json"
	data, err := os.ReadFile(payloadFile)
	if err != nil {
		t.Fatalf("Failed to read payload file: %v", err)
	}

	var payload model.AlertManagerWebhook
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Failed to decode payload: %v", err)
	}

	// Important: Fix up commonLabels just like Prometheus/Alertmanager might
	// In the JSON file I manually added them, but let's verify.

	var buffer bytes.Buffer
	if err := tmpl.Execute(&buffer, payload); err != nil {
		t.Fatalf("Template execution failed: %v", err)
	}

	fmt.Println("----- Template Output -----")
	fmt.Println(buffer.String())
	fmt.Println("---------------------------")
}
