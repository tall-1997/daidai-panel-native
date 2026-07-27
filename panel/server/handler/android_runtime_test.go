package handler

import "testing"

func TestAndroidSupportedRecognizesMagiskEnvMarker(t *testing.T) {
	t.Setenv("DAIDAI_MAGISK_MODULE", "1")

	if !androidSupported() {
		t.Fatal("expected androidSupported to recognize Magisk env marker")
	}
}

func TestResolveAndroidRuntimeBinDirPrefersEnvOverride(t *testing.T) {
	t.Setenv("DAIDAI_ANDROID_RUNTIME_BIN_DIR", "/data/adb/daidai-panel/custom-bin")

	got := resolveAndroidRuntimeBinDir()
	if got != "/data/adb/daidai-panel/custom-bin" {
		t.Fatalf("expected env override bin dir, got %q", got)
	}
}

func TestResolveAndroidRuntimeBinDirFallsBackToDefault(t *testing.T) {
	got := resolveAndroidRuntimeBinDir()
	if got != defaultAndroidRuntimeBinDir {
		t.Fatalf("expected default android runtime bin dir %q, got %q", defaultAndroidRuntimeBinDir, got)
	}
}
