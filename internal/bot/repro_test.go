package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/alertmanager-tg-adapter/internal/model"
)

func TestReproEmptyOutput(t *testing.T) {
	tmpl, err := loadTemplate("../../templates/default.tmpl")
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

	var buffer bytes.Buffer
	if err := tmpl.Execute(&buffer, payload); err != nil {
		t.Fatalf("Template execution failed: %v", err)
	}

	fmt.Println("----- Template Output -----")
	fmt.Println(buffer.String())
	fmt.Println("---------------------------")
}
