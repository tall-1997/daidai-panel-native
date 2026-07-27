package service

import (
	"crypto/sha256"
	"log"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"

	"github.com/google/uuid"
)

const (
	machineCodeConfigKey   = "machine_code"
	machineCodeDescription = "面板机器码（首次启动自动生成，重启/升级不会改变）"
)

// machineCodePattern matches an RFC 4122 v4 UUID in uppercase form, e.g.
// A1B2C3D4-E5F6-4789-ABCD-1234567890AB. Values stored in the database that
// do not match are treated as legacy and regenerated on next startup.
var machineCodePattern = regexp.MustCompile(`^[0-9A-F]{8}-[0-9A-F]{4}-4[0-9A-F]{3}-[89AB][0-9A-F]{3}-[0-9A-F]{12}$`)

var (
	cachedMachineCode string
	machineCodeMu     sync.Mutex
)

func isValidMachineCode(s string) bool {
	return machineCodePattern.MatchString(s)
}

// EnsureMachineCode returns the panel's stable installation identifier.
// The first invocation generates a random value and persists it into the
// system_configs table; subsequent calls (including across process restarts)
// read the stored value, so the code never changes unless the database row
// is explicitly cleared.
func EnsureMachineCode() string {
	machineCodeMu.Lock()
	defer machineCodeMu.Unlock()

	if cachedMachineCode != "" {
		return cachedMachineCode
	}

	if database.DB == nil {
		return ""
	}

	var existing model.SystemConfig
	err := database.DB.Where("`key` = ?", machineCodeConfigKey).First(&existing).Error
	if err == nil {
		if v := strings.TrimSpace(existing.Value); isValidMachineCode(v) {
			cachedMachineCode = v
			return cachedMachineCode
		}
	}

	code := generateMachineCode()
	record := model.SystemConfig{
		Key:         machineCodeConfigKey,
		Value:       code,
		Description: machineCodeDescription,
	}

	if err == nil {
		// Row exists with empty or legacy (non-UUID-v4) value — overwrite it so
		// future startups return the new uppercase UUID format.
		if updateErr := database.DB.Model(&existing).Updates(map[string]interface{}{
			"value":       code,
			"description": machineCodeDescription,
		}).Error; updateErr != nil {
			log.Printf("machine code: migrate existing row failed: %v", updateErr)
		}
	} else if createErr := database.DB.Create(&record).Error; createErr != nil {
		log.Printf("machine code: create row failed: %v", createErr)
		// Fall back to in-memory only so callers still see a stable value
		// for this process lifetime.
	}

	cachedMachineCode = code
	return cachedMachineCode
}

// ResetMachineCodeCacheForTest clears the in-memory cache so tests can
// simulate a fresh process reading an existing database row.
func ResetMachineCodeCacheForTest() {
	machineCodeMu.Lock()
	defer machineCodeMu.Unlock()
	cachedMachineCode = ""
}

func generateMachineCode() string {
	if id, err := uuid.NewRandom(); err == nil {
		return strings.ToUpper(id.String())
	}

	// Fallback only reached if crypto/rand fails (extremely rare). Derive a
	// deterministic 16-byte seed and conform to the RFC 4122 v4 layout so
	// the result is indistinguishable in shape from a real random UUID.
	seed := strings.Join([]string{
		runtime.GOOS,
		runtime.GOARCH,
		hostnameOrUnknown(),
		strconv.Itoa(os.Getpid()),
		time.Now().UTC().Format(time.RFC3339Nano),
	}, "|")
	sum := sha256.Sum256([]byte(seed))
	var raw [16]byte
	copy(raw[:], sum[:16])
	raw[6] = (raw[6] & 0x0F) | 0x40
	raw[8] = (raw[8] & 0x3F) | 0x80
	id, _ := uuid.FromBytes(raw[:])
	return strings.ToUpper(id.String())
}

func hostnameOrUnknown() string {
	if h, err := os.Hostname(); err == nil && strings.TrimSpace(h) != "" {
		return h
	}
	return "unknown"
}
