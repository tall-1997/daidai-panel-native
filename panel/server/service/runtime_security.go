package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type SealedValue struct {
	Provider string `json:"provider"`
	Cipher   []byte `json:"cipher"`
}

type SecretStore interface {
	Seal(context.Context, string, []byte) (SealedValue, error)
	Open(context.Context, string, SealedValue) ([]byte, error)
	Status() SecretStoreStatus
}

type SecretStoreStatus struct {
	Provider  string `json:"provider"`
	Ready     bool   `json:"ready"`
	Version   string `json:"version"`
	Transport string `json:"transport"`
}

type LocalSecretStore struct {
	mu        sync.RWMutex
	aead      cipher.AEAD
	ready     bool
	provider  string
	version   string
	transport string
}

const (
	runtimeMasterKeyEnv      = "DAIDAI_RUNTIME_SECRET_MASTER_KEY"
	runtimeKeystoreKeyEnv    = "DAIDAI_ANDROID_KEYSTORE_MASTER_KEY"
	runtimeMasterKeyFileName = ".runtime_master_key"
	runtimeTrustFileName     = ".runtime_trust_authorizations.json"
)

func (store *LocalSecretStore) Seal(_ context.Context, _ string, payload []byte) (SealedValue, error) {
	store.mu.RLock()
	aead := store.aead
	provider := store.provider
	ready := store.ready
	store.mu.RUnlock()
	if !ready || aead == nil {
		return SealedValue{}, errors.New("local secret store is not initialized")
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return SealedValue{}, fmt.Errorf("generate secret nonce: %w", err)
	}
	ciphertext := aead.Seal(nonce, nonce, payload, nil)
	return SealedValue{Provider: provider, Cipher: ciphertext}, nil
}

func (store *LocalSecretStore) Open(_ context.Context, _ string, value SealedValue) ([]byte, error) {
	store.mu.RLock()
	aead := store.aead
	ready := store.ready
	store.mu.RUnlock()
	if !ready || aead == nil {
		return nil, errors.New("local secret store is not initialized")
	}
	nonceSize := aead.NonceSize()
	if len(value.Cipher) <= nonceSize {
		return nil, errors.New("sealed value payload is invalid")
	}
	nonce := append([]byte{}, value.Cipher[:nonceSize]...)
	payload := append([]byte{}, value.Cipher[nonceSize:]...)
	opened, err := aead.Open(nil, nonce, payload, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt secret payload: %w", err)
	}
	return opened, nil
}

func (store *LocalSecretStore) Status() SecretStoreStatus {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return SecretStoreStatus{
		Provider:  store.provider,
		Ready:     store.ready,
		Version:   store.version,
		Transport: store.transport,
	}
}

func (store *LocalSecretStore) configure(masterKey []byte) error {
	if len(masterKey) != 32 {
		return fmt.Errorf("runtime master key must be 32 bytes, got %d", len(masterKey))
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return fmt.Errorf("initialize runtime cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("initialize runtime gcm: %w", err)
	}
	store.mu.Lock()
	store.aead = aead
	store.ready = true
	store.provider = "local-aes-gcm"
	store.version = "v1"
	store.transport = "in-process"
	store.mu.Unlock()
	return nil
}

type TrustAuthorization struct {
	Source       string   `json:"source"`
	Version      string   `json:"version"`
	SHA256       string   `json:"sha256"`
	Capabilities []string `json:"capabilities"`
	AuthorizedAt string   `json:"authorized_at"`
}

type TrustAuthorizer interface {
	Upsert(TrustAuthorization)
	IsAuthorized(source, version, digest, capability string) bool
	List() []TrustAuthorization
	Status() TrustAuthorizerStatus
}

type TrustAuthorizerStatus struct {
	Provider    string `json:"provider"`
	Ready       bool   `json:"ready"`
	StoragePath string `json:"storage_path,omitempty"`
	RecordCount int    `json:"record_count"`
	LastError   string `json:"last_error,omitempty"`
}

type localTrustAuthorizer struct {
	mu        sync.RWMutex
	records   map[string]TrustAuthorization
	path      string
	ready     bool
	lastError string
}

func (authorizer *localTrustAuthorizer) Upsert(record TrustAuthorization) {
	normalized := normalizeTrustAuthorization(record)
	if normalized.Source == "" || normalized.Version == "" || normalized.SHA256 == "" {
		return
	}
	key := normalized.Source + "|" + normalized.Version + "|" + normalized.SHA256
	authorizer.mu.Lock()
	authorizer.records[key] = normalized
	authorizer.persistLocked()
	authorizer.mu.Unlock()
}

func (authorizer *localTrustAuthorizer) IsAuthorized(source, version, digest, capability string) bool {
	query := normalizeTrustAuthorization(TrustAuthorization{
		Source:       source,
		Version:      version,
		SHA256:       digest,
		Capabilities: []string{capability},
	})
	if !isTrustRecordValid(query) || len(query.Capabilities) == 0 {
		return false
	}
	key := query.Source + "|" + query.Version + "|" + query.SHA256

	authorizer.mu.RLock()
	record, ok := authorizer.records[key]
	authorizer.mu.RUnlock()
	if !ok {
		return false
	}
	for _, allowed := range record.Capabilities {
		if allowed == query.Capabilities[0] {
			return true
		}
	}
	return false
}

func (authorizer *localTrustAuthorizer) List() []TrustAuthorization {
	authorizer.mu.RLock()
	result := make([]TrustAuthorization, 0, len(authorizer.records))
	for _, record := range authorizer.records {
		copyRecord := record
		copyRecord.Capabilities = append([]string{}, record.Capabilities...)
		result = append(result, copyRecord)
	}
	authorizer.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool {
		left := result[i].Source + result[i].Version + result[i].SHA256
		right := result[j].Source + result[j].Version + result[j].SHA256
		return left < right
	})
	return result
}

func (authorizer *localTrustAuthorizer) Status() TrustAuthorizerStatus {
	authorizer.mu.RLock()
	defer authorizer.mu.RUnlock()
	return TrustAuthorizerStatus{
		Provider:    "local-json-file",
		Ready:       authorizer.ready,
		StoragePath: authorizer.path,
		RecordCount: len(authorizer.records),
		LastError:   authorizer.lastError,
	}
}

func (authorizer *localTrustAuthorizer) initialize(dataDir string) error {
	path := filepath.Join(dataDir, runtimeTrustFileName)
	authorizer.mu.Lock()
	defer authorizer.mu.Unlock()
	authorizer.path = path
	authorizer.lastError = ""
	loaded, err := authorizer.readFromDiskLocked(path)
	if err != nil {
		authorizer.ready = false
		authorizer.lastError = err.Error()
		return err
	}
	authorizer.records = loaded
	authorizer.ready = true
	if err := authorizer.persistLocked(); err != nil {
		return err
	}
	return nil
}

func (authorizer *localTrustAuthorizer) readFromDiskLocked(path string) (map[string]TrustAuthorization, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]TrustAuthorization{}, nil
		}
		return nil, fmt.Errorf("read trust authorization store: %w", err)
	}
	if strings.TrimSpace(string(payload)) == "" {
		return map[string]TrustAuthorization{}, nil
	}
	var records []TrustAuthorization
	if err := json.Unmarshal(payload, &records); err != nil {
		return nil, fmt.Errorf("decode trust authorization store: %w", err)
	}
	result := make(map[string]TrustAuthorization, len(records))
	for _, record := range records {
		normalized := normalizeTrustAuthorization(record)
		if !isTrustRecordValid(normalized) {
			continue
		}
		if normalized.AuthorizedAt == "" {
			normalized.AuthorizedAt = time.Now().UTC().Format(time.RFC3339)
		}
		key := normalized.Source + "|" + normalized.Version + "|" + normalized.SHA256
		result[key] = normalized
	}
	return result, nil
}

func (authorizer *localTrustAuthorizer) persistLocked() error {
	if strings.TrimSpace(authorizer.path) == "" {
		authorizer.ready = false
		authorizer.lastError = "trust authorization store path is empty"
		return errors.New(authorizer.lastError)
	}
	entries := make([]TrustAuthorization, 0, len(authorizer.records))
	for _, record := range authorizer.records {
		entries = append(entries, record)
	}
	sort.Slice(entries, func(i, j int) bool {
		left := entries[i].Source + entries[i].Version + entries[i].SHA256
		right := entries[j].Source + entries[j].Version + entries[j].SHA256
		return left < right
	})
	encoded, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		authorizer.ready = false
		authorizer.lastError = err.Error()
		return fmt.Errorf("encode trust authorization store: %w", err)
	}
	tempPath := authorizer.path + ".tmp"
	if err := os.WriteFile(tempPath, encoded, 0o600); err != nil {
		authorizer.ready = false
		authorizer.lastError = err.Error()
		return fmt.Errorf("write trust authorization store: %w", err)
	}
	if err := os.Rename(tempPath, authorizer.path); err != nil {
		authorizer.ready = false
		authorizer.lastError = err.Error()
		return fmt.Errorf("commit trust authorization store: %w", err)
	}
	authorizer.ready = true
	authorizer.lastError = ""
	return nil
}

func normalizeTrustAuthorization(record TrustAuthorization) TrustAuthorization {
	record.Source = strings.TrimSpace(record.Source)
	record.Version = strings.TrimSpace(record.Version)
	record.SHA256 = strings.TrimSpace(strings.ToLower(record.SHA256))
	record.AuthorizedAt = strings.TrimSpace(record.AuthorizedAt)
	if len(record.Capabilities) > 0 {
		normalized := make([]string, 0, len(record.Capabilities))
		seen := map[string]struct{}{}
		for _, capability := range record.Capabilities {
			name := strings.TrimSpace(capability)
			if name == "" {
				continue
			}
			if _, exists := seen[name]; exists {
				continue
			}
			seen[name] = struct{}{}
			normalized = append(normalized, name)
		}
		sort.Strings(normalized)
		record.Capabilities = normalized
	}
	return record
}

func isTrustRecordValid(record TrustAuthorization) bool {
	if record.Source == "" || record.Version == "" || record.SHA256 == "" {
		return false
	}
	if len(record.SHA256) != 64 {
		return false
	}
	for _, ch := range record.SHA256 {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

var (
	runtimeSecretStore SecretStore = &LocalSecretStore{
		provider:  "local-aes-gcm",
		version:   "v1",
		transport: "in-process",
	}
	trustAuthorizer = &localTrustAuthorizer{records: map[string]TrustAuthorization{}}
)

func RuntimeSecretStoreInstance() SecretStore {
	return runtimeSecretStore
}

func SetRuntimeSecretStoreForTest(store SecretStore) func() {
	previous := runtimeSecretStore
	if store == nil {
		runtimeSecretStore = &LocalSecretStore{provider: "local-aes-gcm", version: "v1", transport: "in-process"}
	} else {
		runtimeSecretStore = store
	}
	return func() { runtimeSecretStore = previous }
}

func RuntimeTrustAuthorizer() TrustAuthorizer {
	return trustAuthorizer
}

func InitializeRuntimeSecurity(dataDir string) error {
	return InitializeRuntimeSecurityWithKeystoreKey(dataDir, "")
}

func InitializeRuntimeSecurityWithKeystoreKey(dataDir, keystoreMasterKey string) error {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return errors.New("runtime security dataDir is required")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("create runtime security dataDir: %w", err)
	}
	masterKey, provider, err := loadRuntimeMasterKey(dataDir, keystoreMasterKey)
	if err != nil {
		return err
	}
	store, ok := runtimeSecretStore.(*LocalSecretStore)
	if !ok {
		return errors.New("runtime secret store type is invalid")
	}
	if err := store.configure(masterKey); err != nil {
		return err
	}
	store.mu.Lock()
	store.provider = provider
	store.transport = "in-process"
	if provider == "android-keystore-envelope" {
		store.transport = "android-host-bridge"
	}
	store.mu.Unlock()
	if err := trustAuthorizer.initialize(dataDir); err != nil {
		return err
	}
	return nil
}

func loadRuntimeMasterKey(dataDir, keystoreMasterKey string) ([]byte, string, error) {
	if value := strings.TrimSpace(keystoreMasterKey); value != "" {
		decoded, err := decodeRuntimeMasterKey(value)
		if err != nil {
			return nil, "", fmt.Errorf("decode android keystore bridge key: %w", err)
		}
		return decoded, "android-keystore-envelope", nil
	}
	if envValue := strings.TrimSpace(os.Getenv(runtimeKeystoreKeyEnv)); envValue != "" {
		decoded, err := decodeRuntimeMasterKey(envValue)
		if err != nil {
			return nil, "", fmt.Errorf("decode %s: %w", runtimeKeystoreKeyEnv, err)
		}
		return decoded, "android-keystore-envelope", nil
	}
	if envValue := strings.TrimSpace(os.Getenv(runtimeMasterKeyEnv)); envValue != "" {
		decoded, err := decodeRuntimeMasterKey(envValue)
		if err != nil {
			return nil, "", fmt.Errorf("decode %s: %w", runtimeMasterKeyEnv, err)
		}
		return decoded, "local-aes-gcm", nil
	}
	if runtime.GOOS == "android" {
		return nil, "", errors.New("android keystore runtime key is required")
	}
	keyPath := filepath.Join(dataDir, runtimeMasterKeyFileName)
	payload, err := os.ReadFile(keyPath)
	if err == nil {
		decoded, decodeErr := decodeRuntimeMasterKey(strings.TrimSpace(string(payload)))
		if decodeErr != nil {
			return nil, "", fmt.Errorf("decode runtime master key file: %w", decodeErr)
		}
		return decoded, "local-aes-gcm", nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, "", fmt.Errorf("read runtime master key file: %w", err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, "", fmt.Errorf("generate runtime master key: %w", err)
	}
	encoded := hex.EncodeToString(key)
	if err := os.WriteFile(keyPath, []byte(encoded), 0o600); err != nil {
		return nil, "", fmt.Errorf("persist runtime master key: %w", err)
	}
	return key, "local-aes-gcm", nil
}

func decodeRuntimeMasterKey(raw string) ([]byte, error) {
	if decoded, err := hex.DecodeString(raw); err == nil {
		if len(decoded) != 32 {
			return nil, fmt.Errorf("hex key length=%d", len(decoded))
		}
		return decoded, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("key must be 64-char hex or base64-encoded 32-byte data")
	}
	if len(decoded) != 32 {
		return nil, fmt.Errorf("base64 key length=%d", len(decoded))
	}
	return decoded, nil
}
