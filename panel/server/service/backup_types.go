package service

import (
	"time"

	"daidai-panel/model"
)

type BackupSelection struct {
	Configs       bool `json:"configs"`
	Tasks         bool `json:"tasks"`
	Subscriptions bool `json:"subscriptions"`
	EnvVars       bool `json:"env_vars"`
	Logs          bool `json:"logs"`
	Scripts       bool `json:"scripts"`
	Dependencies  bool `json:"dependencies"`
	TaskViews     bool `json:"task_views"`
}

type BackupCreateOptions struct {
	Password  string
	Name      string
	Selection BackupSelection
}

type BackupUser struct {
	ID           uint       `json:"id"`
	Username     string     `json:"username"`
	PasswordHash string     `json:"password_hash"`
	Role         string     `json:"role"`
	Enabled      bool       `json:"enabled"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type BackupOpenApp struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	AppKey    string    `json:"app_key"`
	AppSecret string    `json:"app_secret"`
	Scopes    string    `json:"scopes"`
	Enabled   bool      `json:"enabled"`
	RateLimit int       `json:"rate_limit"`
	CallCount int64     `json:"call_count,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type BackupNotifyChannel struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Config    string    `json:"config"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type BackupSSHKey struct {
	ID         uint      `json:"id"`
	Name       string    `json:"name"`
	PrivateKey string    `json:"private_key"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type BackupTwoFactorAuth struct {
	UserID    uint      `json:"user_id"`
	Secret    string    `json:"secret"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BackupEnvVar 用指针保存 enabled，区分「老备份缺少 enabled 字段」和「新备份明确 disabled」。
type BackupEnvVar struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	Value     string    `json:"value"`
	Remarks   string    `json:"remarks"`
	Enabled   *bool     `json:"enabled,omitempty"`
	Position  float64   `json:"position"`
	SortOrder int       `json:"sort_order"`
	Group     string    `json:"group"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type BackupDependency struct {
	Type          string `json:"type"`
	Name          string `json:"name"`
	PythonVersion string `json:"python_version,omitempty"`
}

type BackupTaskLog struct {
	TaskID    uint       `json:"task_id"`
	TaskName  string     `json:"task_name"`
	Content   string     `json:"content"`
	Status    *int       `json:"status"`
	Duration  *float64   `json:"duration"`
	LogPath   *string    `json:"log_path"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type BackupConfigBundle struct {
	SystemConfigs     []model.SystemConfig      `json:"system_configs,omitempty"`
	OpenApps          []BackupOpenApp           `json:"open_apps,omitempty"`
	NotifyChannels    []BackupNotifyChannel     `json:"notify_channels,omitempty"`
	Users             []BackupUser              `json:"users,omitempty"`
	IPWhitelists      []model.IPWhitelist       `json:"ip_whitelists,omitempty"`
	TwoFactorAuths    []BackupTwoFactorAuth     `json:"two_factor_auths,omitempty"`
	DependencyMirrors *DependencyMirrorSettings `json:"dependency_mirrors,omitempty"`
}

type BackupPayload struct {
	Configs       BackupConfigBundle   `json:"configs,omitempty"`
	Tasks         []model.Task         `json:"tasks,omitempty"`
	EnvVars       []BackupEnvVar       `json:"env_vars,omitempty"`
	Subscriptions []model.Subscription `json:"subscriptions,omitempty"`
	SSHKeys       []BackupSSHKey       `json:"ssh_keys,omitempty"`
	Dependencies  []BackupDependency   `json:"dependencies,omitempty"`
	TaskLogs      []BackupTaskLog      `json:"task_logs,omitempty"`
	TaskViews     []model.TaskView     `json:"task_views,omitempty"`
}

type BackupManifest struct {
	Format    string          `json:"format"`
	Version   string          `json:"version"`
	Source    string          `json:"source"`
	CreatedAt time.Time       `json:"created_at"`
	Selection BackupSelection `json:"selection"`
	Data      BackupPayload   `json:"data"`
}

func defaultBackupSelection() BackupSelection {
	return BackupSelection{
		Configs:       true,
		Tasks:         true,
		Subscriptions: true,
		EnvVars:       true,
		Logs:          true,
		Scripts:       true,
		Dependencies:  true,
		TaskViews:     true,
	}
}

func (s BackupSelection) Any() bool {
	return s.Configs || s.Tasks || s.Subscriptions || s.EnvVars || s.Logs || s.Scripts || s.Dependencies || s.TaskViews
}

func (s BackupSelection) NormalizeDefaults() BackupSelection {
	if s.Any() {
		return s
	}
	return defaultBackupSelection()
}
