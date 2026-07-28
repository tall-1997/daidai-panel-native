package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type runtimeManifest struct {
	Components []runtimeComponent `json:"components"`
}

type runtimeComponent struct {
	ID         string `json:"id"`
	Entrypoint string `json:"entrypoint"`
	SHA256     string `json:"sha256"`
}

func main() {
	manifestPath := flag.String("manifest", "runtime/manifest.json", "runtime manifest path")
	nativeLibraryDir := flag.String("native-lib-dir", "", "android nativeLibraryDir path")
	strict := flag.Bool("strict", false, "treat placeholder sha256 as failure")
	flag.Parse()

	if strings.TrimSpace(*nativeLibraryDir) == "" {
		fmt.Fprintln(os.Stderr, "missing required flag: --native-lib-dir")
		os.Exit(2)
	}

	manifestPayload, err := os.ReadFile(*manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read manifest: %v\n", err)
		os.Exit(1)
	}

	var manifest runtimeManifest
	if err := json.Unmarshal(manifestPayload, &manifest); err != nil {
		fmt.Fprintf(os.Stderr, "decode manifest: %v\n", err)
		os.Exit(1)
	}
	if len(manifest.Components) == 0 {
		fmt.Fprintln(os.Stderr, "manifest has no components")
		os.Exit(1)
	}

	failed := false
	for _, component := range manifest.Components {
		if component.ID == "" || component.Entrypoint == "" {
			fmt.Fprintf(os.Stderr, "invalid manifest entry: %+v\n", component)
			failed = true
			continue
		}
		entryPath := filepath.Join(*nativeLibraryDir, component.Entrypoint)
		payload, err := os.ReadFile(entryPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s missing: %v\n", component.ID, err)
			failed = true
			continue
		}
		sum := sha256.Sum256(payload)
		hash := hex.EncodeToString(sum[:])
		placeholder := strings.HasPrefix(strings.TrimSpace(component.SHA256), "PLACEHOLDER_SHA256_")
		if placeholder && *strict {
			fmt.Fprintf(os.Stderr, "%s has placeholder sha256 in strict mode\n", component.ID)
			failed = true
			continue
		}
		if !placeholder && !strings.EqualFold(hash, component.SHA256) {
			fmt.Fprintf(os.Stderr, "%s sha256 mismatch: got=%s want=%s\n", component.ID, hash, component.SHA256)
			failed = true
			continue
		}
		fmt.Printf("%s ok (%s)\n", component.ID, component.Entrypoint)
	}

	if failed {
		os.Exit(1)
	}
}
