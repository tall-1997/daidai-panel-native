package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"daidai-panel/model"
)

func SealEnvVarValue(ctx context.Context, env *model.EnvVar, value string) error {
	if env == nil {
		return nil
	}
	sealed, err := RuntimeSecretStoreInstance().Seal(ctx, "env:"+env.Name, []byte(value))
	if err != nil {
		env.Value = value
		env.Secret = false
		env.Sealed = ""
		return nil
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
	if env == nil || !env.Secret || strings.TrimSpace(env.Sealed) == "" {
		if env == nil {
			return ""
		}
		return env.Value
	}
	var payload struct {
		Provider string `json:"provider"`
		Cipher   string `json:"cipher"`
	}
	if err := json.Unmarshal([]byte(env.Sealed), &payload); err != nil {
		return env.Value
	}
	ciphertext, err := base64.StdEncoding.DecodeString(payload.Cipher)
	if err != nil {
		return env.Value
	}
	opened, err := RuntimeSecretStoreInstance().Open(ctx, "env:"+env.Name, SealedValue{Provider: payload.Provider, Cipher: ciphertext})
	if err != nil {
		return env.Value
	}
	return string(opened)
}

func OpenEnvVarForResponse(ctx context.Context, env *model.EnvVar) map[string]interface{} {
	if env == nil {
		return map[string]interface{}{}
	}
	clone := *env
	clone.Value = OpenEnvVarValue(ctx, env)
	return clone.ToDict()
}

func OpenEnvVarsForRuntime(ctx context.Context, envs []model.EnvVar) []model.EnvVar {
	opened := make([]model.EnvVar, len(envs))
	for i := range envs {
		opened[i] = envs[i]
		opened[i].Value = OpenEnvVarValue(ctx, &envs[i])
	}
	return opened
}
