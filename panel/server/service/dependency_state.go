package service

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"daidai-panel/config"
	"daidai-panel/model"
)

func SnapshotDepsToHost() {
	depsDir := filepath.Join(config.C.Data.Dir, "deps")
	persistDir := "/data/adb/daidai-panel/deps-snapshot"

	if _, err := os.Stat(depsDir); err != nil {
		return
	}
	if _, err := os.Stat("/data/adb/daidai-panel"); err != nil {
		return
	}

	cmd := exec.Command("cp", "-rf", depsDir+"/.", persistDir+"/")
	os.MkdirAll(persistDir, 0755)
	cmd.Run()
}

func DependencyInstalled(depType, name string) bool {
	return DependencyInstalledForPythonVersion(depType, name, "")
}

func DependencyInstalledForPythonVersion(depType, name, pythonVersion string) bool {
	name = strings.TrimSpace(name)
	if depType == "" || name == "" {
		return false
	}

	depsDir := filepath.Join(config.C.Data.Dir, "deps")
	switch depType {
	case model.DepTypeNodeJS:
		modDir := filepath.Join(depsDir, "nodejs", "node_modules", filepath.FromSlash(NormalizeNodeDependencyPackageName(name)))
		if info, err := os.Stat(modDir); err == nil {
			if !info.IsDir() {
				return false
			}
			requested, constrained := exactNodeDependencyVersion(name)
			if !constrained {
				return !strings.ContainsAny(nodeDependencyVersionSpec(name), "<>=~^*")
			}
			return requested == nodeDependencyInstalledVersion(modDir)
		}
	case model.DepTypePython:
		pythonVersion = NormalizeDependencyPythonVersion(pythonVersion)
		if installed, ok := pythonDistributionVersion(ManagedPythonSitePackagesDir(pythonVersion), name); ok {
			requested, constrained := exactPythonDependencyVersion(name)
			if constrained {
				return requested == installed
			}
			return !strings.ContainsAny(name, "<>=!~")
		}
		candidates := []string{
			ResolveManagedPipBinaryForPythonVersion(pythonVersion),
			filepath.Join(ManagedPythonVenvDir(pythonVersion), "bin", "pip"),
			filepath.Join(ManagedPythonVenvDir(pythonVersion), "bin", "pip3"),
			filepath.Join(ManagedPythonVenvDir(pythonVersion), "Scripts", "pip.exe"),
			filepath.Join(ManagedPythonVenvDir(pythonVersion), "Scripts", "pip3.exe"),
		}
		for _, pipBin := range candidates {
			pipBin = strings.TrimSpace(pipBin)
			if pipBin == "" {
				continue
			}
			if _, err := os.Stat(pipBin); err == nil {
				showCmd := androidManagedCommand(pipBin, []string{"show", name}, "")
				showCmd.Env = SanitizePipEnv(os.Environ())
				androidFinalizeCommand(showCmd)
				if out, err := showCmd.CombinedOutput(); err == nil && strings.Contains(string(out), "Name:") {
					return pythonShowVersionSatisfies(name, string(out))
				}
			}
		}
		showCmd, err := NewPipCommandForPythonVersion(pythonVersion, []string{"show", name})
		if err != nil {
			return false
		}
		showCmd.Env = SanitizePipEnv(os.Environ())
		androidFinalizeCommand(showCmd)
		if out, err := showCmd.CombinedOutput(); err == nil && strings.Contains(string(out), "Name:") {
			return pythonShowVersionSatisfies(name, string(out))
		}
	case model.DepTypeLinux:
		if _, ok := managedRuntimeBinary(name); ok {
			return true
		}
		for _, probe := range []struct {
			binary string
			args   []string
		}{
			{binary: "apk", args: []string{"info", "-e", name}},
			{binary: "dpkg-query", args: []string{"-W", "-f=${Status}", name}},
			{binary: "rpm", args: []string{"-q", name}},
		} {
			bin := probe.binary
			if _, err := exec.LookPath(bin); err != nil {
				if rt := androidContainer(); rt.active {
					guest := rt.rootfsBinary(bin)
					if guest == "" {
						continue
					}
					bin = guest
				} else {
					continue
				}
			}
			probeCmd := androidManagedCommand(bin, probe.args, "")
			if out, err := probeCmd.CombinedOutput(); err == nil {
				if probe.binary != "dpkg-query" || strings.Contains(string(out), "install ok installed") {
					return true
				}
			}
		}
	}

	return false
}

func pythonDistributionInstalled(sitePackages, name string) bool {
	_, ok := pythonDistributionVersion(sitePackages, name)
	return ok
}

func pythonDistributionVersion(sitePackages, name string) (string, bool) {
	entries, err := os.ReadDir(sitePackages)
	if err != nil {
		return "", false
	}
	want := canonicalPythonDependencyName(name)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".dist-info") {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), ".dist-info")
		if index := strings.LastIndex(base, "-"); index > 0 {
			base = base[:index]
		}
		if canonicalPythonDependencyName(base) == want {
			metadata, err := os.ReadFile(filepath.Join(sitePackages, entry.Name(), "METADATA"))
			if err == nil {
				for _, line := range strings.Split(string(metadata), "\n") {
					if strings.HasPrefix(line, "Version:") {
						return strings.TrimSpace(strings.TrimPrefix(line, "Version:")), true
					}
				}
			}
			if index := strings.LastIndex(strings.TrimSuffix(entry.Name(), ".dist-info"), "-"); index > 0 {
				return strings.TrimSuffix(entry.Name(), ".dist-info")[index+1:], true
			}
			return "", true
		}
	}
	return "", false
}

func exactPythonDependencyVersion(spec string) (string, bool) {
	for _, separator := range []string{"===", "=="} {
		if index := strings.Index(spec, separator); index >= 0 {
			version := strings.TrimSpace(strings.SplitN(spec[index+len(separator):], ";", 2)[0])
			return version, version != ""
		}
	}
	return "", false
}

func pythonShowVersionSatisfies(spec, output string) bool {
	requested, exact := exactPythonDependencyVersion(spec)
	if !exact {
		return !strings.ContainsAny(spec, "<>=!~")
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "Version:") {
			return requested == strings.TrimSpace(strings.TrimPrefix(line, "Version:"))
		}
	}
	return false
}

func nodeDependencyVersionSpec(spec string) string {
	name := NormalizeNodeDependencyPackageName(spec)
	return strings.TrimPrefix(strings.TrimSpace(spec[len(name):]), "@")
}

func exactNodeDependencyVersion(spec string) (string, bool) {
	version := nodeDependencyVersionSpec(spec)
	if version == "" || version == "latest" || strings.ContainsAny(version, "<>=~^*") {
		return "", false
	}
	return version, true
}

func nodeDependencyInstalledVersion(moduleDir string) string {
	payload, err := os.ReadFile(filepath.Join(moduleDir, "package.json"))
	if err != nil {
		return ""
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(payload, &manifest) != nil {
		return ""
	}
	return strings.TrimSpace(manifest.Version)
}

func NormalizeNodeDependencyPackageName(spec string) string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return ""
	}

	if strings.HasPrefix(spec, "@") {
		parts := strings.SplitN(spec, "/", 2)
		if len(parts) != 2 {
			return spec
		}
		scope := strings.TrimSpace(parts[0])
		rest := strings.TrimSpace(parts[1])
		if scope == "" || rest == "" {
			return spec
		}
		if idx := strings.LastIndex(rest, "@"); idx > 0 {
			rest = rest[:idx]
		}
		if rest == "" {
			return spec
		}
		return scope + "/" + rest
	}

	if idx := strings.LastIndex(spec, "@"); idx > 0 {
		return spec[:idx]
	}
	return spec
}
