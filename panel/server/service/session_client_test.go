package service

import "testing"

func TestDetectSessionClientInfoBuildsAppDisplayNameFromHeaders(t *testing.T) {
	info := DetectSessionClientInfo(
		"app",
		"daidai-panel-app",
		"android",
		"Xiaomi 15 Pro",
		"umi",
		"15",
		"Dart/3.11 (dart:io)",
	)

	if info.Type != SessionClientApp {
		t.Fatalf("expected app type, got %q", info.Type)
	}
	if got := SessionClientDisplayName(info); got != "App端 · Xiaomi 15 Pro · Android 15" {
		t.Fatalf("unexpected app display name: %q", got)
	}
}

func TestResolveStoredSessionClientNameFallsBackToBrowserDisplay(t *testing.T) {
	got := ResolveStoredSessionClientName(
		SessionClientWeb,
		"",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.36",
	)

	if got != "网页端 · Chrome · Windows" {
		t.Fatalf("unexpected web display name: %q", got)
	}
}
