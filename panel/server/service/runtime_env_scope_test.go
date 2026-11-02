package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// seedEnvScopeFixtures 造一组覆盖「按名 / 按分组 / 多分组 / 同名多条 / 禁用」的变量，再写一份 config.sh。
func seedEnvScopeFixtures(t *testing.T) {
	t.Helper()

	rows := []model.EnvVar{
		{Name: "JD_COOKIE", Value: "jd-1", Group: "京东", Position: 1000},
		{Name: "JD_COOKIE", Value: "jd-2", Group: "京东", Position: 2000},
		{Name: "WX_TOKEN", Value: "wx", Group: "微信", Position: 3000},
		{Name: "MULTI_GROUP", Value: "multi", Group: "日常,联通", Position: 4000},
		{Name: "LONELY", Value: "lonely", Position: 5000},
		{Name: "DISABLED_JD", Value: "off", Group: "京东", Position: 6000},
	}
	for i := range rows {
		rows[i].Enabled = true
		if err := database.DB.Create(&rows[i]).Error; err != nil {
			t.Fatalf("create env %s: %v", rows[i].Name, err)
		}
	}
	// Enabled 是 default:true，Create 时 false 会落成 true，只能建完再补写（database-guidelines.md）
	if err := database.DB.Model(&model.EnvVar{}).Where("name = ?", "DISABLED_JD").Update("enabled", false).Error; err != nil {
		t.Fatalf("disable DISABLED_JD: %v", err)
	}
	if err := os.WriteFile(filepath.Join(config.C.Data.Dir, "config.sh"), []byte("export FROM_CONFIG='config-value'\n"), 0o600); err != nil {
		t.Fatalf("write config.sh: %v", err)
	}
}

func buildScopedEnvForTest(t *testing.T, scope *RuntimeEnvScope) (map[string]string, *middleware.Claims) {
	t.Helper()

	// 与 TestManagedRuntimeEnvMapCarriesScriptAPIEntrypoint 同样用测试环境的脚本目录，保证通知 helper 那段能建出来
	scriptsDir := config.C.Data.ScriptsDir
	envMap, tokenInfo, err := BuildManagedRuntimeEnvMapWithScriptToken(scriptsDir, scriptsDir, nil, time.Hour, "", scope)
	if err != nil {
		t.Fatalf("build managed runtime env map: %v", err)
	}
	if tokenInfo == nil || envMap["DAIDAI_TOKEN"] == "" {
		t.Fatalf("expected DAIDAI_TOKEN to stay injected, got token=%#v", tokenInfo)
	}
	claims, err := middleware.ParseToken(envMap["DAIDAI_TOKEN"])
	if err != nil {
		t.Fatalf("parse DAIDAI_TOKEN: %v", err)
	}
	return envMap, claims
}

func assertUserEnvKeys(t *testing.T, envMap map[string]string, present, absent []string) {
	t.Helper()
	for _, key := range present {
		if _, ok := envMap[key]; !ok {
			t.Fatalf("expected %s to be injected, env keys=%v", key, envMapKeysForTest(envMap))
		}
	}
	for _, key := range absent {
		if value, ok := envMap[key]; ok {
			t.Fatalf("expected %s NOT to be injected, got %q", key, value)
		}
	}
}

func envMapKeysForTest(envMap map[string]string) []string {
	keys := make([]string, 0, len(envMap))
	for key := range envMap {
		keys = append(keys, key)
	}
	return keys
}

// #168：「全部」（nil）与改动前一致——所有已启用变量都注入、禁用的不注入，脚本令牌仍是普通 operator 令牌。
func TestRuntimeEnvScopeNilInjectsAllEnabledVars(t *testing.T) {
	testutil.SetupTestEnv(t)
	seedEnvScopeFixtures(t)

	envMap, claims := buildScopedEnvForTest(t, nil)
	assertUserEnvKeys(t, envMap, []string{"JD_COOKIE", "WX_TOKEN", "MULTI_GROUP", "LONELY", "FROM_CONFIG"}, []string{"DISABLED_JD"})
	if got := envMap["JD_COOKIE"]; got != "jd-1&jd-2" {
		t.Fatalf("同名多条应照旧用 & 合并，实际 %q", got)
	}
	if claims.NotifyOnly {
		t.Fatal("「全部」时的脚本令牌不应带 notify_only")
	}
	if claims.Role != "operator" {
		t.Fatalf("脚本令牌角色应仍是 operator，实际 %q", claims.Role)
	}
}

// #168：按变量名指定，只注入点名的已启用变量（同名多条照样合并），config.sh 与 DAIDAI_* 照常注入，令牌收紧成只能发通知。
func TestRuntimeEnvScopeSelectedByName(t *testing.T) {
	testutil.SetupTestEnv(t)
	seedEnvScopeFixtures(t)

	scope := &RuntimeEnvScope{Names: []string{"JD_COOKIE", "DISABLED_JD"}}
	envMap, claims := buildScopedEnvForTest(t, scope)
	assertUserEnvKeys(t, envMap, []string{"JD_COOKIE", "FROM_CONFIG", "DAIDAI_API_BASE", "TZ"}, []string{"WX_TOKEN", "MULTI_GROUP", "LONELY", "DISABLED_JD"})
	if got := envMap["JD_COOKIE"]; got != "jd-1&jd-2" {
		t.Fatalf("同名多条应照旧用 & 合并，实际 %q", got)
	}
	if got := envMap["FROM_CONFIG"]; got != "config-value" {
		t.Fatalf("config.sh 里的变量应照常注入，实际 %q", got)
	}
	if !claims.NotifyOnly {
		t.Fatal("指定范围时脚本令牌必须是 notify-only")
	}
	if strings.Join(scope.InjectedNames, ",") != "JD_COOKIE" {
		t.Fatalf("InjectedNames 应只回填实际注入的用户变量名，实际 %v", scope.InjectedNames)
	}
}

// #168：按分组指定，一条变量属于多个分组时任一命中就算；禁用的变量即便分组命中也不注入。
func TestRuntimeEnvScopeSelectedByGroup(t *testing.T) {
	testutil.SetupTestEnv(t)
	seedEnvScopeFixtures(t)

	envMap, _ := buildScopedEnvForTest(t, &RuntimeEnvScope{Groups: []string{"京东", "联通"}})
	assertUserEnvKeys(t, envMap, []string{"JD_COOKIE", "MULTI_GROUP"}, []string{"WX_TOKEN", "LONELY", "DISABLED_JD"})
}

// #168：分组与变量名取并集。
func TestRuntimeEnvScopeSelectedUnionOfGroupsAndNames(t *testing.T) {
	testutil.SetupTestEnv(t)
	seedEnvScopeFixtures(t)

	scope := &RuntimeEnvScope{Groups: []string{"微信"}, Names: []string{"LONELY"}}
	envMap, _ := buildScopedEnvForTest(t, scope)
	assertUserEnvKeys(t, envMap, []string{"WX_TOKEN", "LONELY"}, []string{"JD_COOKIE", "MULTI_GROUP", "DISABLED_JD"})
	if strings.Join(scope.InjectedNames, ",") != "WX_TOKEN,LONELY" {
		t.Fatalf("InjectedNames 应按注入顺序列出，实际 %v", scope.InjectedNames)
	}
}

// #168：指定但两个列表都空 = 一个用户变量都不注入；config.sh、DAIDAI_TOKEN 仍在。
func TestRuntimeEnvScopeSelectedWithEmptyListsInjectsNoUserVars(t *testing.T) {
	testutil.SetupTestEnv(t)
	seedEnvScopeFixtures(t)

	scope := &RuntimeEnvScope{}
	envMap, claims := buildScopedEnvForTest(t, scope)
	assertUserEnvKeys(t, envMap, []string{"FROM_CONFIG", "DAIDAI_NOTIFY_URL"}, []string{"JD_COOKIE", "WX_TOKEN", "MULTI_GROUP", "LONELY", "DISABLED_JD"})
	if !claims.NotifyOnly {
		t.Fatal("指定范围（即便为空）时脚本令牌必须是 notify-only")
	}
	if len(scope.InjectedNames) != 0 {
		t.Fatalf("两个列表都空时不该注入任何用户变量，实际 %v", scope.InjectedNames)
	}
}

// TaskRuntimeEnvScope 只认 selected；all、空串、手工改坏的值都按全部（nil）。
func TestTaskRuntimeEnvScopeOnlySelectedReturnsScope(t *testing.T) {
	for _, value := range []string{"", "all", "ALL", "bogus"} {
		if scope := TaskRuntimeEnvScope(&model.Task{EnvScope: value, EnvNames: "A"}); scope != nil {
			t.Fatalf("env_scope=%q 应按全部处理（nil），实际 %#v", value, scope)
		}
	}
	scope := TaskRuntimeEnvScope(&model.Task{EnvScope: "selected", EnvGroups: "京东,日常", EnvNames: "A,B"})
	if scope == nil {
		t.Fatal("env_scope=selected 应返回范围")
	}
	if strings.Join(scope.Groups, ",") != "京东,日常" || strings.Join(scope.Names, ",") != "A,B" {
		t.Fatalf("范围解析不对：%#v", scope)
	}
}

// 任务日志开头那一行：只写名字，最多列 20 个，超出的写个数。
func TestFormatTaskEnvScopeLogLine(t *testing.T) {
	if got := formatTaskEnvScopeLogLine([]string{"A", "B", "C"}); got != "[环境变量] 已按任务设置只注入 3 个：A、B、C\n" {
		t.Fatalf("unexpected line: %q", got)
	}
	if got := formatTaskEnvScopeLogLine(nil); !strings.HasPrefix(got, "[环境变量] 已按任务设置只注入 0 个") {
		t.Fatalf("unexpected empty line: %q", got)
	}
	names := make([]string, 0, 25)
	for i := 1; i <= 25; i++ {
		names = append(names, fmt.Sprintf("V%d", i))
	}
	got := formatTaskEnvScopeLogLine(names)
	if !strings.HasPrefix(got, "[环境变量] 已按任务设置只注入 25 个：V1、") {
		t.Fatalf("unexpected long line: %q", got)
	}
	if !strings.Contains(got, "V20 等，另有 5 个未列出") || strings.Contains(got, "V21") {
		t.Fatalf("超过 20 个时只列前 20 个、其余写个数，实际 %q", got)
	}
}

// #168：备份清单直接序列化 model.Task，三个字段原样进 manifest.json、原样恢复；
// 老备份没有这三个键 → 恢复成「全部」。
func TestBackupRoundTripKeepsTaskEnvScope(t *testing.T) {
	testutil.SetupTestEnv(t)

	task := &model.Task{
		Name:           "限定变量任务",
		Command:        "task demo.py",
		CronExpression: "0 0 * * *",
		Status:         model.TaskStatusEnabled,
		EnvScope:       model.TaskEnvScopeSelected,
		EnvGroups:      "京东,日常",
		EnvNames:       "JD_COOKIE",
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	filePath, err := CreateBackup(BackupCreateOptions{Selection: BackupSelection{Tasks: true}})
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}
	manifestData := readBackupManifestJSONForTest(t, filePath)

	var payload map[string]interface{}
	if err := json.Unmarshal(manifestData, &payload); err != nil {
		t.Fatalf("decode manifest as map: %v", err)
	}
	data, _ := payload["data"].(map[string]interface{})
	rawTasks, _ := data["tasks"].([]interface{})
	if len(rawTasks) != 1 {
		t.Fatalf("期望清单里有 1 个任务，实际 %d 个", len(rawTasks))
	}
	firstTask, _ := rawTasks[0].(map[string]interface{})
	if firstTask["env_scope"] != "selected" || firstTask["env_groups"] != "京东,日常" || firstTask["env_names"] != "JD_COOKIE" {
		t.Fatalf("清单里的环境变量范围不对：scope=%#v groups=%#v names=%#v", firstTask["env_scope"], firstTask["env_groups"], firstTask["env_names"])
	}

	var manifest BackupManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if err := database.DB.Exec("DELETE FROM tasks").Error; err != nil {
		t.Fatalf("clear tasks: %v", err)
	}
	if err := restoreBackupManifest(manifest, t.TempDir()); err != nil {
		t.Fatalf("restore backup manifest: %v", err)
	}
	var restored model.Task
	if err := database.DB.Where("name = ?", "限定变量任务").First(&restored).Error; err != nil {
		t.Fatalf("load restored task: %v", err)
	}
	if restored.EnvScope != model.TaskEnvScopeSelected || restored.EnvGroups != "京东,日常" || restored.EnvNames != "JD_COOKIE" {
		t.Fatalf("恢复后环境变量范围不对：%q / %q / %q", restored.EnvScope, restored.EnvGroups, restored.EnvNames)
	}

	// 老备份：tasks 里根本没有这三个键
	raw := `{
		"format": "daidai-panel-backup",
		"version": "0.4.0",
		"source": "daidai-panel",
		"selection": {"tasks": true},
		"data": {
			"tasks": [
				{"id": 5, "name": "老备份任务", "command": "task legacy.py", "cron_expression": "0 0 * * *", "status": 1}
			]
		}
	}`
	var legacy BackupManifest
	if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
		t.Fatalf("decode legacy manifest: %v", err)
	}
	if err := restoreBackupManifest(legacy, t.TempDir()); err != nil {
		t.Fatalf("老备份必须能正常恢复，实际失败：%v", err)
	}
	var legacyTask model.Task
	if err := database.DB.Where("name = ?", "老备份任务").First(&legacyTask).Error; err != nil {
		t.Fatalf("load restored legacy task: %v", err)
	}
	if legacyTask.GetEnvScope() != model.TaskEnvScopeAll || legacyTask.EnvGroups != "" || legacyTask.EnvNames != "" {
		t.Fatalf("老备份没有这三个键，恢复后应为全部，实际 %q / %q / %q", legacyTask.EnvScope, legacyTask.EnvGroups, legacyTask.EnvNames)
	}
	if legacyTask.EnvScope != model.TaskEnvScopeAll {
		t.Fatalf("老备份恢复后库里应落默认值 all，实际 %q", legacyTask.EnvScope)
	}
}
