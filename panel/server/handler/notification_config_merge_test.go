package handler

import (
	"encoding/json"
	"testing"
)

func TestPreserveRedactedNotificationFields(t *testing.T) {
	merged, err := preserveRedactedNotificationFields(
		`{"token":"real-token","topic":"old"}`,
		`{"token":"********","topic":"new"}`,
	)
	if err != nil {
		t.Fatalf("merge config: %v", err)
	}
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(merged), &config); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if config["token"] != "real-token" || config["topic"] != "new" {
		t.Fatalf("unexpected merged config: %#v", config)
	}
}

func TestPreserveRedactedNotificationFieldsDropsUnknownPlaceholder(t *testing.T) {
	merged, err := preserveRedactedNotificationFields(`{"url":"https://example.com"}`, `{"token":"********","url":"https://example.com"}`)
	if err != nil {
		t.Fatalf("merge config: %v", err)
	}
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(merged), &config); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if _, exists := config["token"]; exists {
		t.Fatalf("unknown placeholder must not be persisted: %#v", config)
	}
}

func TestNormalizeNotificationChannelTypeSupportsPushPlusTypo(t *testing.T) {
	if got := normalizeNotificationChannelType(" PludPlus "); got != "pushplus" {
		t.Fatalf("normalized type = %q, want pushplus", got)
	}
}
