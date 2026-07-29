package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	RuntimeIDYaegiGo             = "yaegi-go"
	RuntimeIDGoBuilderAndroidARM = "go-builder-android-arm64"
)

type RuntimeExecutable struct {
	RuntimeID        string
	Path             string
	NativeLibraryDir string
	Entrypoint       string
	Capabilities     []string
}

type RuntimeLocator interface {
	Resolve(runtimeID string) (RuntimeExecutable, error)
}

type ManifestRuntimeLocator struct {
	nativeLibraryDir string
	manifest         RuntimeManifest
}

func NewManifestRuntimeLocator(nativeLibraryDir string, manifest RuntimeManifest) *ManifestRuntimeLocator {
	return &ManifestRuntimeLocator{nativeLibraryDir: strings.TrimSpace(nativeLibraryDir), manifest: manifest}
}

func (locator *ManifestRuntimeLocator) Resolve(runtimeID string) (RuntimeExecutable, error) {
	runtimeID = strings.TrimSpace(runtimeID)
	if runtimeID == "" {
		return RuntimeExecutable{}, errors.New("runtime id is required")
	}
	for _, component := range locator.manifest.Components {
		if component.ID != runtimeID {
			continue
		}
		path, err := resolveRuntimeEntrypointPath(locator.nativeLibraryDir, component.Entrypoint)
		if err != nil {
			return RuntimeExecutable{}, err
		}
		return RuntimeExecutable{
			RuntimeID:        component.ID,
			Path:             path,
			NativeLibraryDir: locator.nativeLibraryDir,
			Entrypoint:       component.Entrypoint,
			Capabilities:     append([]string{}, component.Capabilities...),
		}, nil
	}
	return RuntimeExecutable{}, fmt.Errorf("runtime id not found: %s", runtimeID)
}

type defaultRuntimeLocator struct{}

func (defaultRuntimeLocator) Resolve(runtimeID string) (RuntimeExecutable, error) {
	baseline := RuntimeComponentBaselineSnapshot()
	manifest := RuntimeManifest{Components: make([]RuntimeManifestComponent, 0, len(baseline.Components))}
	for _, component := range baseline.Components {
		if component.ID == runtimeID && !component.Verified {
			return RuntimeExecutable{}, runtimeBlockedError(runtimeID, component.Reason)
		}
		manifest.Components = append(manifest.Components, RuntimeManifestComponent{ID: component.ID, Entrypoint: component.Entrypoint})
	}
	for _, suite := range baseline.SmokeSuites {
		if suite.RuntimeID != runtimeID {
			continue
		}
		if len(suite.Checks) == 0 {
			return RuntimeExecutable{}, runtimeBlockedError(runtimeID, "smoke-evidence-missing")
		}
		for _, check := range suite.Checks {
			if check.Status != "pass" {
				reason := check.Reason
				if reason == "" {
					reason = "smoke-" + check.Status
				}
				return RuntimeExecutable{}, runtimeBlockedError(runtimeID, reason)
			}
		}
	}
	if len(manifest.Components) == 0 {
		manager := NewRuntimeComponentManager(baseline.NativeLibraryDir)
		if strings.TrimSpace(manager.nativeLibraryDir) == "" {
			manager.nativeLibraryDir = strings.TrimSpace(os.Getenv("DAIDAI_NATIVE_LIBRARY_DIR"))
		}
		loaded, err := manager.readManifest()
		if err != nil {
			return RuntimeExecutable{}, err
		}
		manifest = loaded
		baseline.NativeLibraryDir = manager.nativeLibraryDir
	}
	return NewManifestRuntimeLocator(baseline.NativeLibraryDir, manifest).Resolve(runtimeID)
}

func runtimeBlockedError(runtimeID, reason string) error {
	if reason == "" {
		reason = "component-not-verified"
	}
	return fmt.Errorf("%w: %s: %s", ErrRuntimeBlocked, runtimeID, reason)
}

var (
	runtimeLocatorMu sync.RWMutex
	runtimeLocator   RuntimeLocator = defaultRuntimeLocator{}
)

func RuntimeLocatorInstance() RuntimeLocator {
	runtimeLocatorMu.RLock()
	defer runtimeLocatorMu.RUnlock()
	return runtimeLocator
}

func SetRuntimeLocatorForTest(locator RuntimeLocator) func() {
	runtimeLocatorMu.Lock()
	previous := runtimeLocator
	if locator == nil {
		runtimeLocator = defaultRuntimeLocator{}
	} else {
		runtimeLocator = locator
	}
	runtimeLocatorMu.Unlock()
	return func() {
		runtimeLocatorMu.Lock()
		runtimeLocator = previous
		runtimeLocatorMu.Unlock()
	}
}

func resolveRuntimeEntrypointPath(nativeLibraryDir, entrypoint string) (string, error) {
	baseDir := filepath.Clean(strings.TrimSpace(nativeLibraryDir))
	if baseDir == "" || baseDir == "." {
		return "", errors.New("native library dir is empty")
	}
	realBase, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return "", err
	}
	entrypoint = strings.TrimSpace(entrypoint)
	if entrypoint == "" {
		return "", errors.New("entrypoint is empty")
	}
	cleaned := filepath.Clean(entrypoint)
	if cleaned == "." || cleaned == string(filepath.Separator) || strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
		return "", errors.New("entrypoint escapes native library dir")
	}
	resolved := filepath.Clean(filepath.Join(realBase, cleaned))
	realResolved, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(realBase, realResolved)
	if err != nil {
		return "", err
	}
	relative = filepath.Clean(relative)
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("entrypoint escapes native library dir")
	}
	return realResolved, nil
}
