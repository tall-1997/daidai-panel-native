package service

import "testing"

func TestParseBackupSelectionCSV(t *testing.T) {
	selection, err := parseBackupSelectionCSV("configs,tasks,scripts")
	if err != nil {
		t.Fatalf("parse backup selection: %v", err)
	}
	if !selection.Configs || !selection.Tasks || !selection.Scripts {
		t.Fatalf("expected configs/tasks/scripts enabled, got %+v", selection)
	}
	if selection.Logs || selection.EnvVars || selection.Subscriptions || selection.Dependencies {
		t.Fatalf("expected unspecified selections disabled, got %+v", selection)
	}
}

func TestBackupScheduleCronExpression(t *testing.T) {
	cases := []struct {
		name string
		cfg  BackupScheduleConfig
		want string
	}{
		{
			name: "daily",
			cfg: BackupScheduleConfig{
				Frequency: "daily",
				Time:      "03:30",
			},
			want: "0 30 3 * * *",
		},
		{
			name: "weekly",
			cfg: BackupScheduleConfig{
				Frequency: "weekly",
				Time:      "01:05",
				Weekday:   "6",
			},
			want: "0 5 1 * * 6",
		},
		{
			name: "monthly",
			cfg: BackupScheduleConfig{
				Frequency: "monthly",
				Time:      "22:00",
				Monthday:  15,
			},
			want: "0 0 22 15 * *",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := backupScheduleCronExpression(tc.cfg)
			if err != nil {
				t.Fatalf("backup schedule cron expression: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
