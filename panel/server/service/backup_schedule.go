package service

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"daidai-panel/model"

	"github.com/robfig/cron/v3"
)

type BackupScheduleConfig struct {
	Enabled   bool
	Frequency string
	Time      string
	Weekday   string
	Monthday  int
	Name      string
	Password  string
	Selection BackupSelection
}

type BackupScheduler struct {
	cron    *cron.Cron
	entryID cron.EntryID
	mu      sync.Mutex
	running bool
}

var globalBackupScheduler *BackupScheduler

func InitBackupScheduler() {
	scheduler := &BackupScheduler{
		cron: cron.New(cron.WithSeconds(), cron.WithChain(cron.Recover(cron.DefaultLogger))),
	}
	scheduler.cron.Start()
	globalBackupScheduler = scheduler
	scheduler.Reload()
	log.Println("backup scheduler initialized")
}

func ShutdownBackupScheduler() {
	if globalBackupScheduler == nil {
		return
	}
	ctx := globalBackupScheduler.cron.Stop()
	<-ctx.Done()
	log.Println("backup scheduler stopped")
}

func GetBackupScheduler() *BackupScheduler {
	return globalBackupScheduler
}

func ReloadBackupScheduler() {
	if globalBackupScheduler == nil {
		return
	}
	globalBackupScheduler.Reload()
}

func (s *BackupScheduler) Reload() {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.entryID != 0 {
		s.cron.Remove(s.entryID)
		s.entryID = 0
	}

	cfg, err := loadBackupScheduleConfig()
	if err != nil {
		log.Printf("backup scheduler config invalid: %v", err)
		return
	}
	if !cfg.Enabled {
		return
	}

	cronExpr, err := backupScheduleCronExpression(cfg)
	if err != nil {
		log.Printf("backup scheduler cron invalid: %v", err)
		return
	}

	entryID, err := s.cron.AddFunc(cronExpr, func() {
		s.runScheduledBackup()
	})
	if err != nil {
		log.Printf("backup scheduler add job failed: %v", err)
		return
	}

	s.entryID = entryID
}

func (s *BackupScheduler) runScheduledBackup() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		log.Println("backup scheduler skipped: previous backup still running")
		return
	}
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	cfg, err := loadBackupScheduleConfig()
	if err != nil {
		log.Printf("backup scheduler load config failed: %v", err)
		return
	}
	if !cfg.Enabled {
		return
	}

	filePath, err := CreateBackup(BackupCreateOptions{
		Password:  cfg.Password,
		Name:      buildScheduledBackupName(cfg),
		Selection: cfg.Selection,
	})
	if err != nil {
		log.Printf("backup scheduler create backup failed: %v", err)
		return
	}

	log.Printf("backup scheduler created backup: %s", filePath)

	cleanupOldScheduledBackups(cfg, 3)
}

func cleanupOldScheduledBackups(cfg BackupScheduleConfig, retentionDays int) {
	backups, err := ListBackups()
	if err != nil {
		log.Printf("backup cleanup: list backups failed: %v", err)
		return
	}

	prefix := strings.TrimSpace(cfg.Name)
	if prefix == "" {
		prefix = "scheduled-backup"
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	deleted := 0

	for _, b := range backups {
		name, _ := b["name"].(string)
		if name == "" || !strings.HasPrefix(name, prefix) {
			continue
		}
		createdAt, ok := b["created_at"].(time.Time)
		if !ok {
			continue
		}
		if createdAt.Before(cutoff) {
			if err := DeleteBackup(name); err != nil {
				log.Printf("backup cleanup: delete %s failed: %v", name, err)
			} else {
				deleted++
			}
		}
	}

	if deleted > 0 {
		log.Printf("backup cleanup: deleted %d old scheduled backup(s) older than %d days", deleted, retentionDays)
	}
}

func loadBackupScheduleConfig() (BackupScheduleConfig, error) {
	selection, err := parseBackupSelectionCSV(model.GetRegisteredConfig("backup_schedule_selection"))
	if err != nil {
		return BackupScheduleConfig{}, err
	}

	cfg := BackupScheduleConfig{
		Enabled:   model.GetRegisteredConfigBool("backup_schedule_enabled"),
		Frequency: strings.ToLower(strings.TrimSpace(model.GetRegisteredConfig("backup_schedule_frequency"))),
		Time:      strings.TrimSpace(model.GetRegisteredConfig("backup_schedule_time")),
		Weekday:   strings.TrimSpace(model.GetRegisteredConfig("backup_schedule_weekday")),
		Monthday:  model.GetRegisteredConfigInt("backup_schedule_monthday"),
		Name:      strings.TrimSpace(model.GetRegisteredConfig("backup_schedule_name")),
		Password:  model.GetRegisteredConfig("backup_schedule_password"),
		Selection: selection.NormalizeDefaults(),
	}

	if cfg.Frequency == "" {
		cfg.Frequency = "daily"
	}
	if cfg.Time == "" {
		cfg.Time = "03:00"
	}
	if cfg.Monthday <= 0 {
		cfg.Monthday = 1
	}
	return cfg, nil
}

func parseBackupSelectionCSV(raw string) (BackupSelection, error) {
	allowed := map[string]func(*BackupSelection){
		"configs":       func(s *BackupSelection) { s.Configs = true },
		"tasks":         func(s *BackupSelection) { s.Tasks = true },
		"subscriptions": func(s *BackupSelection) { s.Subscriptions = true },
		"env_vars":      func(s *BackupSelection) { s.EnvVars = true },
		"logs":          func(s *BackupSelection) { s.Logs = true },
		"scripts":       func(s *BackupSelection) { s.Scripts = true },
		"dependencies":  func(s *BackupSelection) { s.Dependencies = true },
		"task_views":    func(s *BackupSelection) { s.TaskViews = true },
	}

	var selection BackupSelection
	for _, token := range strings.FieldsFunc(strings.TrimSpace(raw), func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	}) {
		key := strings.ToLower(strings.TrimSpace(token))
		if key == "" {
			continue
		}
		apply, ok := allowed[key]
		if !ok {
			return BackupSelection{}, fmt.Errorf("invalid backup selection item: %s", key)
		}
		apply(&selection)
	}
	return selection, nil
}

func backupScheduleCronExpression(cfg BackupScheduleConfig) (string, error) {
	hour, minute, err := parseBackupScheduleClock(cfg.Time)
	if err != nil {
		return "", err
	}

	switch cfg.Frequency {
	case "daily":
		return fmt.Sprintf("0 %d %d * * *", minute, hour), nil
	case "weekly":
		weekday := strings.TrimSpace(cfg.Weekday)
		if weekday == "" {
			weekday = "1"
		}
		return fmt.Sprintf("0 %d %d * * %s", minute, hour, weekday), nil
	case "monthly":
		day := cfg.Monthday
		if day < 1 || day > 28 {
			return "", fmt.Errorf("invalid monthly day: %d", day)
		}
		return fmt.Sprintf("0 %d %d %d * *", minute, hour, day), nil
	default:
		return "", fmt.Errorf("invalid backup frequency: %s", cfg.Frequency)
	}
}

func parseBackupScheduleClock(raw string) (hour int, minute int, err error) {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid backup time: %s", raw)
	}

	hour, err = strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("invalid backup hour: %s", raw)
	}
	minute, err = strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("invalid backup minute: %s", raw)
	}

	return hour, minute, nil
}

func buildScheduledBackupName(cfg BackupScheduleConfig) string {
	prefix := strings.TrimSpace(cfg.Name)
	if prefix == "" {
		prefix = "scheduled-backup"
	}
	return fmt.Sprintf("%s_%s", prefix, time.Now().Format("20060102_150405"))
}
