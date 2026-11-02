package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"daidai-panel/model"
)

const RedactedEnvSecretValue = "********"

func SealEnvVarValue(ctx context.Context, env *model.EnvVar, value string) error {
	if env == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sealed, err := RuntimeSecretStoreInstance().Seal(ctx, "env:"+env.Name, []byte(value))
	if err != nil {
		env.Value = ""
		env.Secret = false
		env.Sealed = ""
		return fmt.Errorf("seal env var value: %w", err)
	}
	payload, err := json.Marshal(struct {
		Provider string `json:"provider"`
		Cipher   string `json:"cipher"`
	}{Provider: sealed.Provider, Cipher: base64.StdEncoding.EncodeToString(sealed.Cipher)})
	if err != nil {
		return err
	}
	env.Value = ""
	env.Secret = true
	env.Sealed = string(payload)
	return nil
}

func OpenEnvVarValue(ctx context.Context, env *model.EnvVar) string {
	opened, err := OpenEnvVarValueStrict(ctx, env)
	if err != nil {
		return ""
	}
	return opened
}

func OpenEnvVarValueStrict(ctx context.Context, env *model.EnvVar) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if env == nil || !env.Secret {
		if env == nil {
			return "", nil
		}
		return env.Value, nil
	}
	if strings.TrimSpace(env.Sealed) == "" {
		return "", fmt.Errorf("sealed env var %s is empty", env.Name)
	}
	var payload struct {
		Provider string `json:"provider"`
		Cipher   string `json:"cipher"`
	}
	if err := json.Unmarshal([]byte(env.Sealed), &payload); err != nil {
		return "", fmt.Errorf("parse sealed env var %s: %w", env.Name, err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(payload.Cipher)
	if err != nil {
		return "", fmt.Errorf("decode sealed env var %s: %w", env.Name, err)
	}
	opened, err := RuntimeSecretStoreInstance().Open(ctx, "env:"+env.Name, SealedValue{Provider: payload.Provider, Cipher: ciphertext})
	if err != nil {
		return "", fmt.Errorf("open sealed env var %s: %w", env.Name, err)
	}
	return string(opened), nil
}

func OpenEnvVarForResponse(env *model.EnvVar) map[string]interface{} {
	if env == nil {
		return map[string]interface{}{}
	}
	clone := *env
	if clone.Secret || strings.TrimSpace(clone.Sealed) != "" || clone.Value != "" {
		clone.Value = RedactedEnvSecretValue
	}
	return clone.ToDict()
}

func OpenEnvVarsForRuntime(ctx context.Context, envs []model.EnvVar) []model.EnvVar {
	opened := make([]model.EnvVar, 0, len(envs))
	for i := range envs {
		value, err := OpenEnvVarValueStrict(ctx, &envs[i])
		if err != nil {
			continue
		}
		env := envs[i]
		env.Value = value
		opened = append(opened, env)
	}
	return opened
}
