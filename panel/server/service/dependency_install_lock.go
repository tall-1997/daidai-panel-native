package service

import (
	"strings"
	"sync"

	"daidai-panel/model"
)

type dependencyInstallLockEntry struct {
	mutex      sync.Mutex
	references int
}

var dependencyInstallLocks = struct {
	sync.Mutex
	entries map[string]*dependencyInstallLockEntry
}{entries: make(map[string]*dependencyInstallLockEntry)}
var dependencyRecordMu sync.Mutex

func lockDependencyRecord() func() {
	dependencyRecordMu.Lock()
	return dependencyRecordMu.Unlock
}

func dependencyInstallKey(depType, name, runtimeVersion string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch depType {
	case model.DepTypePython:
		normalized = CanonicalizePythonPackageName(name)
		runtimeVersion = NormalizeDependencyPythonVersion(runtimeVersion)
	case model.DepTypeNodeJS:
		normalized = strings.ToLower(NormalizeNodeDependencyPackageName(name))
		runtimeVersion = "node"
	default:
		runtimeVersion = "system"
	}
	return depType + "\x00" + normalized + "\x00" + runtimeVersion
}

// LockDependencyInstall serializes one normalized dependency while allowing unrelated installs to proceed.
func LockDependencyInstall(depType, name, runtimeVersion string) func() {
	key := dependencyInstallKey(depType, name, runtimeVersion)
	dependencyInstallLocks.Lock()
	entry := dependencyInstallLocks.entries[key]
	if entry == nil {
		entry = &dependencyInstallLockEntry{}
		dependencyInstallLocks.entries[key] = entry
	}
	entry.references++
	dependencyInstallLocks.Unlock()
	entry.mutex.Lock()
	return func() {
		entry.mutex.Unlock()
		dependencyInstallLocks.Lock()
		entry.references--
		if entry.references == 0 {
			delete(dependencyInstallLocks.entries, key)
		}
		dependencyInstallLocks.Unlock()
	}
}
