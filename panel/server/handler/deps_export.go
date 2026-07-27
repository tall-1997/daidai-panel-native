package handler

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/service"
)

func buildDependencyExportText(depType string, deps []model.Dependency) (string, error) {
	lines, err := buildDependencyExportLines(depType, deps)
	if err != nil {
		return "", err
	}
	return strings.Join(lines, "\n"), nil
}

func buildDependencyExportLines(depType string, deps []model.Dependency) ([]string, error) {
	pythonVersion := ""
	if depType == model.DepTypePython && len(deps) > 0 {
		pythonVersion = service.NormalizeDependencyPythonVersion(deps[0].PythonVersion)
	}
	versionMap, err := resolveDependencyVersions(depType, pythonVersion)
	if err != nil {
		return nil, err
	}
	return buildDependencyExportLinesFromVersions(deps, versionMap), nil
}

func buildDependencyExportLinesFromVersions(deps []model.Dependency, versions map[string]string) []string {
	lines := make([]string, 0, len(deps))
	for _, dep := range deps {
		version := strings.TrimSpace(versions[strings.ToLower(dep.Name)])
		if version == "" {
			version = "未知版本"
		}
		lines = append(lines, fmt.Sprintf("%s==>%s", dep.Name, version))
	}
	return lines
}

func resolveDependencyVersions(depType, pythonVersion string) (map[string]string, error) {
	switch depType {
	case model.DepTypeNodeJS:
		return resolveNodeDependencyVersions()
	case model.DepTypePython:
		return resolvePythonDependencyVersions(pythonVersion)
	case model.DepTypeLinux:
		return resolveLinuxDependencyVersions()
	default:
		return nil, fmt.Errorf("不支持的依赖类型: %s", depType)
	}
}

func resolveNodeDependencyVersions() (map[string]string, error) {
	type npmDependency struct {
		Version string `json:"version"`
	}
	type npmListResponse struct {
		Dependencies map[string]npmDependency `json:"dependencies"`
	}

	depsDir := filepath.Join(config.C.Data.Dir, "deps", "nodejs")
	out, err := exec.Command("npm", "list", "--prefix", depsDir, "--json", "--depth=0").Output()
	if err != nil {
		return nil, err
	}

	var payload npmListResponse
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, err
	}

	result := make(map[string]string, len(payload.Dependencies))
	for name, dep := range payload.Dependencies {
		result[strings.ToLower(name)] = strings.TrimSpace(dep.Version)
	}
	return result, nil
}

func resolvePythonDependencyVersions(pythonVersion string) (map[string]string, error) {
	type pipPackage struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}

	pipBin := service.ResolveManagedPipBinaryForPythonVersion(pythonVersion)
	var listCmd *exec.Cmd
	if strings.TrimSpace(pipBin) == "" {
		var err error
		listCmd, err = service.NewPipCommandForPythonVersion(pythonVersion, []string{"list", "--format=json"})
		if err != nil {
			return nil, err
		}
	} else {
		listCmd = exec.Command(pipBin, "list", "--format=json")
	}
	listCmd.Env = service.SanitizePipEnv(os.Environ())
	out, err := listCmd.Output()
	if err != nil {
		return nil, err
	}

	var packages []pipPackage
	if err := json.Unmarshal(out, &packages); err != nil {
		return nil, err
	}

	result := make(map[string]string, len(packages))
	for _, pkg := range packages {
		result[strings.ToLower(pkg.Name)] = strings.TrimSpace(pkg.Version)
	}
	return result, nil
}

func resolveLinuxDependencyVersions() (map[string]string, error) {
	manager, err := detectLinuxPackageManager()
	if err != nil {
		return nil, err
	}

	var deps []model.Dependency
	database.DB.Where("type = ? AND status = ?", model.DepTypeLinux, model.DepStatusInstalled).Find(&deps)

	names := make([]string, 0, len(deps))
	seen := make(map[string]struct{}, len(deps))
	for _, dep := range deps {
		key := strings.ToLower(strings.TrimSpace(dep.Name))
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		names = append(names, dep.Name)
	}

	return resolveLinuxVersionsByNames(manager, names)
}

func resolveLinuxVersionsByNames(manager linuxPackageManager, names []string) (map[string]string, error) {
	result := make(map[string]string, len(names))
	sortedNames := append([]string(nil), names...)
	slices.Sort(sortedNames)

	for _, name := range sortedNames {
		version, err := readLinuxPackageVersion(manager, name)
		if err != nil {
			return nil, err
		}
		result[strings.ToLower(name)] = version
	}
	return result, nil
}

func readLinuxPackageVersion(manager linuxPackageManager, packageName string) (string, error) {
	switch manager.Name {
	case "apk":
		out, err := exec.Command(manager.Binary, "info", "-v", packageName).Output()
		if err != nil {
			return "", err
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if trimmed, ok := strings.CutPrefix(line, packageName+"-"); ok {
				return strings.TrimSpace(trimmed), nil
			}
			return line, nil
		}
		return "", nil
	case "apt":
		out, err := exec.Command("dpkg-query", "-W", "-f=${Version}", packageName).Output()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	case "dnf", "yum", "microdnf", "zypper":
		out, err := exec.Command("rpm", "-q", "--qf", "%{VERSION}-%{RELEASE}", packageName).Output()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	default:
		return "", fmt.Errorf("不支持的 Linux 包管理器: %s", manager.Binary)
	}
}
