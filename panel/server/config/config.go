package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	JWT      JWTConfig      `yaml:"jwt"`
	Data     DataConfig     `yaml:"data"`
	CORS     CORSConfig     `yaml:"cors"`
}

type ServerConfig struct {
	Port   int    `yaml:"port"`
	Mode   string `yaml:"mode"`
	WebDir string `yaml:"web_dir"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type JWTConfig struct {
	Secret             string        `yaml:"secret"`
	AccessTokenExpire  time.Duration `yaml:"access_token_expire"`
	RefreshTokenExpire time.Duration `yaml:"refresh_token_expire"`
}

type DataConfig struct {
	Dir        string `yaml:"dir"`
	ScriptsDir string `yaml:"scripts_dir"`
	LogDir     string `yaml:"log_dir"`
}

type CORSConfig struct {
	Origins []string `yaml:"origins"`
}

var C *Config

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	if envPort := os.Getenv("SERVER_PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			cfg.Server.Port = p
		}
	}
	if envDBPath := os.Getenv("DB_PATH"); envDBPath != "" {
		cfg.Database.Path = envDBPath
	}
	if envWebDir := os.Getenv("WEB_DIR"); envWebDir != "" {
		cfg.Server.WebDir = envWebDir
	}

	if cfg.JWT.AccessTokenExpire == 0 {
		cfg.JWT.AccessTokenExpire = 480 * time.Hour
	}
	if cfg.JWT.RefreshTokenExpire == 0 {
		cfg.JWT.RefreshTokenExpire = 1440 * time.Hour
	}

	// 路径解析锚点：以 config.yaml 所在目录为基准，而不是 cwd。
	// cwd 在 Docker WORKDIR / systemd WorkingDirectory / Windows 双击启动 /
	// 用户从其他目录拉起二进制等场景下都不稳定，会导致相对路径里的 sqlite
	// 数据库被建到错误位置（v2.2.5 → v2.2.6 升级后"数据丢失"事故的根因）。
	// 以 config.yaml 所在目录为锚则始终稳定，且符合"路径相对 config 文件"的直觉。
	configDir, err := filepath.Abs(filepath.Dir(path))
	if err != nil || configDir == "" {
		configDir, _ = os.Getwd()
	}

	cfg.Data.Dir = resolveDataPath(configDir, cfg.Data.Dir)
	cfg.Data.ScriptsDir = resolveDataPath(configDir, cfg.Data.ScriptsDir)
	cfg.Data.LogDir = resolveDataPath(configDir, cfg.Data.LogDir)
	// Database.Path 必须一并解析。v2.2.6 引入 Data.* 的相对→绝对转换时漏了
	// 这一项，导致 sqlite 仍按 cwd 解析相对路径，是数据丢失事故的代码层根因。
	cfg.Database.Path = resolveDataPath(configDir, cfg.Database.Path)

	os.MkdirAll(cfg.Data.Dir, 0755)
	os.MkdirAll(cfg.Data.ScriptsDir, 0755)
	os.MkdirAll(cfg.Data.LogDir, 0755)

	// 密钥必须落在已解析为绝对路径的数据目录里：若在 resolveDataPath 之前生成，
	// 相对 data.dir 会按 cwd 解析，导致 cwd 变化时每次启动生成新密钥，所有 token 立即失效，
	// 且 CLI 与 server 会各持一把互不相认的密钥。
	if cfg.JWT.Secret == "" {
		secret, secretErr := loadOrGenerateSecret(cfg.Data.Dir)
		if secretErr != nil {
			return nil, fmt.Errorf("初始化 JWT 密钥失败: %w", secretErr)
		}
		cfg.JWT.Secret = secret
	}

	log.Printf("config loaded: path=%s db=%s data_dir=%s", path, cfg.Database.Path, cfg.Data.Dir)

	C = cfg
	return cfg, nil
}

func resolveDataPath(baseDir, raw string) string {
	trimmed := filepath.Clean(strings.TrimSpace(raw))
	if trimmed == "" || trimmed == "." {
		return trimmed
	}
	if filepath.IsAbs(trimmed) {
		return trimmed
	}
	if baseDir == "" {
		abs, err := filepath.Abs(trimmed)
		if err != nil {
			return trimmed
		}
		return abs
	}
	return filepath.Clean(filepath.Join(baseDir, trimmed))
}

func loadOrGenerateSecret(dataDir string) (string, error) {
	secretFile := filepath.Join(dataDir, ".jwt_secret")
	if data, err := os.ReadFile(secretFile); err == nil && len(data) > 0 {
		return string(data), nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成随机密钥失败: %w", err)
	}
	secret := hex.EncodeToString(b)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", fmt.Errorf("创建数据目录 %s 失败: %w", dataDir, err)
	}
	if err := os.WriteFile(secretFile, []byte(secret), 0600); err != nil {
		return "", fmt.Errorf("写入密钥文件 %s 失败: %w", secretFile, err)
	}
	return secret, nil
}
