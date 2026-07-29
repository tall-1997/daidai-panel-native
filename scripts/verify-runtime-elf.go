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

type smokeEvidence struct {
	Matrix  []string      `json:"matrix"`
	Records []smokeRecord `json:"records"`
}

type smokeRecord struct {
	RuntimeID      string       `json:"runtime_id"`
	Version        string       `json:"version"`
	Entry          string       `json:"entry"`
	IsolationLevel string       `json:"isolation_level"`
	TimeoutSeconds int          `json:"timeout_seconds"`
	Checks         []smokeCheck `json:"checks"`
}

type smokeCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Output string `json:"output"`
}

type runtimeComponent struct {
	ID         string `json:"id"`
	Entrypoint string `json:"entrypoint"`
	SHA256     string `json:"sha256"`
}

func main() {
	manifestPath := flag.String("manifest", "runtime/manifest.json", "runtime manifest path")
	smokeEvidencePath := flag.String("smoke-evidence", "runtime/smoke-evidence.json", "runtime smoke evidence path")
	nativeLibraryDir := flag.String("native-lib-dir", "", "android nativeLibraryDir path")
	strict := flag.Bool("strict", true, "treat placeholder sha256 as failure")
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
	evidence, evidenceErr := readSmokeEvidence(*smokeEvidencePath)
	if evidenceErr != nil {
		fmt.Fprintf(os.Stderr, "read smoke evidence: %v\n", evidenceErr)
		os.Exit(1)
	}
	evidenceByRuntime := map[string]smokeRecord{}
	for _, record := range evidence.Records {
		evidenceByRuntime[record.RuntimeID] = record
	}

	failed := false
	for _, component := range manifest.Components {
		if component.ID == "" || component.Entrypoint == "" {
			fmt.Fprintf(os.Stderr, "invalid manifest entry: %+v\n", component)
			failed = true
			continue
		}
		entryPath, pathErr := resolveEntrypointPath(*nativeLibraryDir, component.Entrypoint)
		if pathErr != nil {
			fmt.Fprintf(os.Stderr, "%s invalid entrypoint: %v\n", component.ID, pathErr)
			failed = true
			continue
		}
		payload, err := os.ReadFile(entryPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s missing: %v\n", component.ID, err)
			failed = true
			continue
		}
		if err := validateRuntimeELF(payload); err != nil {
			fmt.Fprintf(os.Stderr, "%s invalid elf: %v\n", component.ID, err)
			failed = true
			continue
		}
		if bytesContains(payload, []byte("RUNTIME_STUB_OK")) {
			fmt.Fprintf(os.Stderr, "%s is a runtime stub, not a real interpreter\n", component.ID)
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
		if err := validateSmokeRecord(component, evidenceByRuntime[component.ID]); err != nil {
			fmt.Fprintf(os.Stderr, "%s smoke evidence invalid: %v\n", component.ID, err)
			failed = true
			continue
		}
		fmt.Printf("%s ok (%s)\n", component.ID, component.Entrypoint)
	}

	if failed {
		os.Exit(1)
	}
}

func bytesContains(payload, marker []byte) bool {
	if len(marker) == 0 || len(payload) < len(marker) {
		return false
	}
	for i := 0; i <= len(payload)-len(marker); i++ {
		matched := true
		for j := range marker {
			if payload[i+j] != marker[j] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func readSmokeEvidence(path string) (smokeEvidence, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return smokeEvidence{}, err
	}
	var evidence smokeEvidence
	if err := json.Unmarshal(payload, &evidence); err != nil {
		return smokeEvidence{}, err
	}
	if len(evidence.Matrix) == 0 || len(evidence.Records) == 0 {
		return smokeEvidence{}, fmt.Errorf("empty evidence")
	}
	return evidence, nil
}

func validateSmokeRecord(component runtimeComponent, record smokeRecord) error {
	if record.RuntimeID != component.ID {
		return fmt.Errorf("missing runtime record")
	}
	if record.Entry != component.Entrypoint {
		return fmt.Errorf("entry mismatch")
	}
	if record.Version == "" || record.IsolationLevel == "" || record.TimeoutSeconds <= 0 || len(record.Checks) == 0 {
		return fmt.Errorf("incomplete runtime record")
	}
	for _, check := range record.Checks {
		if check.ID == "" || check.Status != "pass" || strings.TrimSpace(check.Output) == "" {
			return fmt.Errorf("incomplete check %q", check.ID)
		}
	}
	return nil
}

func validateRuntimeELF(payload []byte) error {
	if len(payload) < 20 {
		return fmt.Errorf("payload too small")
	}
	if payload[0] != 0x7f || payload[1] != 'E' || payload[2] != 'L' || payload[3] != 'F' {
		return fmt.Errorf("not an ELF file")
	}
	if payload[4] != 2 {
		return fmt.Errorf("not 64-bit ELF")
	}
	if payload[5] != 1 {
		return fmt.Errorf("not little-endian ELF")
	}
	machine := int(payload[18]) | int(payload[19])<<8
	if machine != 183 {
		return fmt.Errorf("not AArch64 ELF")
	}
	return nil
}

func resolveEntrypointPath(baseDir, entrypoint string) (string, error) {
	baseDir = filepath.Clean(strings.TrimSpace(baseDir))
	if baseDir == "" || baseDir == "." {
		return "", fmt.Errorf("native library dir is empty")
	}
	entrypoint = strings.TrimSpace(entrypoint)
	if entrypoint == "" {
		return "", fmt.Errorf("entrypoint is empty")
	}
	cleaned := filepath.Clean(entrypoint)
	if cleaned == "." || cleaned == string(filepath.Separator) || strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("entrypoint escapes native library dir")
	}
	resolved := filepath.Clean(filepath.Join(baseDir, cleaned))
	relative, err := filepath.Rel(baseDir, resolved)
	if err != nil {
		return "", err
	}
	relative = filepath.Clean(relative)
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("entrypoint escapes native library dir")
	}
	return resolved, nil
}
