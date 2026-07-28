package service

import (
	"context"
	"sort"
	"strings"
	"sync"
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

type LocalSecretStore struct{}

func (store *LocalSecretStore) Seal(_ context.Context, _ string, payload []byte) (SealedValue, error) {
	copyPayload := append([]byte{}, payload...)
	return SealedValue{Provider: "local-placeholder", Cipher: copyPayload}, nil
}

func (store *LocalSecretStore) Open(_ context.Context, _ string, value SealedValue) ([]byte, error) {
	copyPayload := append([]byte{}, value.Cipher...)
	return copyPayload, nil
}

func (store *LocalSecretStore) Status() SecretStoreStatus {
	return SecretStoreStatus{
		Provider:  "local-placeholder",
		Ready:     true,
		Version:   "v0",
		Transport: "in-process",
	}
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
	List() []TrustAuthorization
}

type localTrustAuthorizer struct {
	mu      sync.RWMutex
	records map[string]TrustAuthorization
}

func (authorizer *localTrustAuthorizer) Upsert(record TrustAuthorization) {
	normalized := normalizeTrustAuthorization(record)
	if normalized.Source == "" || normalized.Version == "" || normalized.SHA256 == "" {
		return
	}
	key := normalized.Source + "|" + normalized.Version + "|" + normalized.SHA256
	authorizer.mu.Lock()
	authorizer.records[key] = normalized
	authorizer.mu.Unlock()
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

func normalizeTrustAuthorization(record TrustAuthorization) TrustAuthorization {
	record.Source = strings.TrimSpace(record.Source)
	record.Version = strings.TrimSpace(record.Version)
	record.SHA256 = strings.TrimSpace(strings.ToLower(record.SHA256))
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

var (
	runtimeSecretStore SecretStore = &LocalSecretStore{}
	trustAuthorizer                = &localTrustAuthorizer{records: map[string]TrustAuthorization{}}
)

func RuntimeSecretStoreInstance() SecretStore {
	return runtimeSecretStore
}

func RuntimeTrustAuthorizer() TrustAuthorizer {
	return trustAuthorizer
}
