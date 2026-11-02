package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// TestBackupPayloadModelsHaveNoJSONHiddenFields 是 issue #112 的护栏。
//
// 备份清单直接序列化 model.X 时，X 上任何 json:"-" 的字段都会被静默丢掉：
// model.Task.Labels（任务标签）和 model.Subscription.AuthToken（订阅 PAT）就是这么丢的 ——
// 导出成功、恢复成功、日志无异常，只有用户自己发现数据没了。
//
// 现在这两处改成「嵌入 model.X + 外层同名字段覆盖」，代价是以后再给 model 加一个 json:"-" 字段
// 又会静默丢，所以在这里用反射把不变量钉死：
// BackupPayload / BackupConfigBundle 里凡是直连或嵌入 model.X 的，
// X 不得存在未被外层覆盖的 json:"-" 字段。
func TestBackupPayloadModelsHaveNoJSONHiddenFields(t *testing.T) {
	const modelPkgPath = "daidai-panel/model"

	// unwrap 把 []T / *T / [][]T 之类剥到底层结构体类型。
	unwrap := func(typ reflect.Type) reflect.Type {
		for {
			switch typ.Kind() {
			case reflect.Ptr, reflect.Slice, reflect.Array:
				typ = typ.Elem()
			default:
				return typ
			}
		}
	}
	// jsonName 取 json tag 的第一段（"-" 表示不参与序列化）。
	jsonName := func(field reflect.StructField) string {
		return strings.Split(field.Tag.Get("json"), ",")[0]
	}

	visited := map[reflect.Type]bool{}
	var walk func(typ reflect.Type, path string)
	walk = func(typ reflect.Type, path string) {
		typ = unwrap(typ)
		if typ.Kind() != reflect.Struct || visited[typ] {
			return
		}
		visited[typ] = true

		// 外层自己声明的、参与序列化的字段名。
		// 嵌入 model 里被 json:"-" 藏起来的字段，只有被这些同名字段接住才算修好。
		covered := map[string]bool{}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.Anonymous || field.PkgPath != "" || jsonName(field) == "-" {
				continue
			}
			covered[field.Name] = true
		}

		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			fieldType := unwrap(field.Type)
			if fieldType.Kind() != reflect.Struct {
				continue
			}
			childPath := path + "." + field.Name

			if fieldType.PkgPath() == modelPkgPath {
				for j := 0; j < fieldType.NumField(); j++ {
					inner := fieldType.Field(j)
					if inner.PkgPath != "" || jsonName(inner) != "-" {
						continue
					}
					// 直连（例如 []model.SystemConfig）时外层没有任何覆盖字段，一律算丢；
					// 嵌入（BackupTask{model.Task; Labels}）时看外层有没有同名字段接住。
					if field.Anonymous && covered[inner.Name] {
						continue
					}
					t.Errorf("%s 用到的 %s.%s 带 json:\"-\"，写进 manifest.json 时会被静默丢掉；"+
						"请把它包成带同名可序列化字段的 Backup* 结构（参考 BackupTask.Labels / BackupSubscription.AuthToken）",
						childPath, fieldType.Name(), inner.Name)
				}
			}

			walk(fieldType, childPath)
		}
	}

	walk(reflect.TypeOf(BackupPayload{}), "BackupPayload")
	walk(reflect.TypeOf(BackupConfigBundle{}), "BackupConfigBundle")
}

// TestBackupRoundTripKeepsTaskLabelsAndSubscriptionAuthToken 走完整往返：
// 建数据 → CreateBackup 打包 → 从 tar 里读 manifest.json → 反序列化 → 恢复 → 查库。
// 既钉住「清单里 labels 是 JSON 数组」的形态，也钉住恢复后标签与 PAT 真的还原。
func TestBackupRoundTripKeepsTaskLabelsAndSubscriptionAuthToken(t *testing.T) {
	testutil.SetupTestEnv(t)

	subscription := &model.Subscription{
		Name:      "带令牌订阅",
		Type:      model.SubTypeGitRepo,
		URL:       "https://example.com/demo.git",
		AuthType:  model.SubAuthTypeToken,
		AuthToken: "ghp_backup_round_trip_token",
		Enabled:   true,
	}
	if err := database.DB.Create(subscription).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	task := &model.Task{
		Name:           "带标签任务",
		Command:        "task demo.py",
		CronExpression: "0 0 * * *",
		Status:         model.TaskStatusEnabled,
	}
	// 三类标签各来一个：分组标签、用户自定义标签、订阅归属标签。
	task.SetLabelsFromSlice([]string{"分组:工作", "我的标签", subscriptionTaskLabel(subscription.ID)})
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	filePath, err := CreateBackup(BackupCreateOptions{
		Selection: BackupSelection{Tasks: true, Subscriptions: true},
	})
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}
	manifestData := readBackupManifestJSONForTest(t, filePath)

	// 刻意先解成 map 而不是 BackupManifest：结构体会把逗号串也读成合法值，
	// 测不出「labels 退回成字符串」这种形态回归。
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
	rawLabels, ok := firstTask["labels"].([]interface{})
	if !ok {
		t.Fatalf("manifest.json 里 tasks[0].labels 必须是数组，实际 %#v", firstTask["labels"])
	}
	exported := make([]string, 0, len(rawLabels))
	for _, item := range rawLabels {
		text, _ := item.(string)
		exported = append(exported, text)
	}
	wantExported := []string{"分组:工作", "我的标签", subscriptionTaskLabel(subscription.ID)}
	if strings.Join(exported, "|") != strings.Join(wantExported, "|") {
		t.Fatalf("导出的标签不对：期望 %v，实际 %v", wantExported, exported)
	}

	rawSubs, _ := data["subscriptions"].([]interface{})
	if len(rawSubs) != 1 {
		t.Fatalf("期望清单里有 1 个订阅，实际 %d 个", len(rawSubs))
	}
	firstSub, _ := rawSubs[0].(map[string]interface{})
	if got, _ := firstSub["auth_token"].(string); got != "ghp_backup_round_trip_token" {
		t.Fatalf("订阅 PAT 没进备份包，实际 %q", got)
	}

	var manifest BackupManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if err := restoreBackupManifest(manifest, t.TempDir()); err != nil {
		t.Fatalf("restore backup manifest: %v", err)
	}

	var restoredSub model.Subscription
	if err := database.DB.Where("name = ?", "带令牌订阅").First(&restoredSub).Error; err != nil {
		t.Fatalf("load restored subscription: %v", err)
	}
	if restoredSub.AuthToken != "ghp_backup_round_trip_token" {
		t.Fatalf("恢复后订阅 PAT 丢了，实际 %q", restoredSub.AuthToken)
	}

	var restoredTask model.Task
	if err := database.DB.Where("name = ?", "带标签任务").First(&restoredTask).Error; err != nil {
		t.Fatalf("load restored task: %v", err)
	}
	// 订阅在恢复时会拿到新主键，订阅归属标签必须跟着走。
	wantLabels := []string{"分组:工作", "我的标签", subscriptionTaskLabel(restoredSub.ID)}
	assertTaskLabelsEqualForTest(t, restoredTask.GetLabels(), wantLabels)
}

// TestRestoreBackupManifestRemapsSubscriptionLabelsToNewIDs 钉住 design §1.5 的核心：
// 订阅 ID 发生偏移时，subscription:<旧ID> 必须重映射到新 ID；没命中的直接丢弃，绝不保留旧 ID。
//
// 不做重映射比原 bug 更糟：旧 ID 可能正好是另一个不相干订阅的新主键，
// 那条订阅同步的 autoDelete 分支会把认领到、却不在候选集里的任务连同日志物理删除。
func TestRestoreBackupManifestRemapsSubscriptionLabelsToNewIDs(t *testing.T) {
	testutil.SetupTestEnv(t)

	manifest := BackupManifest{
		Format:    "daidai-panel-backup",
		Version:   "0.4.0",
		Source:    "daidai-panel",
		Selection: BackupSelection{Tasks: true, Subscriptions: true},
		Data: BackupPayload{
			Tasks: []BackupTask{
				{
					Task: model.Task{
						Name:           "订阅任务",
						Command:        "task sub.py",
						CronExpression: "0 0 * * *",
						Status:         model.TaskStatusEnabled,
					},
					Labels: []string{"分组:工作", "我的标签", "subscription:7"},
				},
				{
					Task: model.Task{
						Name:           "孤儿订阅任务",
						Command:        "task orphan.py",
						CronExpression: "0 1 * * *",
						Status:         model.TaskStatusEnabled,
					},
					// 备份里根本没有 id=99 的订阅：这条标签必须被丢弃，其余标签原样保留。
					Labels: []string{"保留标签", "subscription:99"},
				},
			},
			Subscriptions: []BackupSubscription{
				{
					Subscription: model.Subscription{
						ID:      7,
						Name:    "备份订阅",
						Type:    model.SubTypeGitRepo,
						URL:     "https://example.com/demo.git",
						Enabled: true,
					},
					AuthToken: "ghp_remap_token",
				},
			},
		},
	}

	if err := restoreBackupManifest(manifest, t.TempDir()); err != nil {
		t.Fatalf("restore backup manifest: %v", err)
	}

	var restoredSub model.Subscription
	if err := database.DB.Where("name = ?", "备份订阅").First(&restoredSub).Error; err != nil {
		t.Fatalf("load restored subscription: %v", err)
	}
	if restoredSub.ID == 7 {
		t.Fatalf("测试前提失效：恢复后的订阅 ID 必须与备份里的 7 不同，实际 %d", restoredSub.ID)
	}
	if restoredSub.AuthToken != "ghp_remap_token" {
		t.Fatalf("恢复后订阅 PAT 丢了，实际 %q", restoredSub.AuthToken)
	}

	var subTask model.Task
	if err := database.DB.Where("name = ?", "订阅任务").First(&subTask).Error; err != nil {
		t.Fatalf("load restored subscription task: %v", err)
	}
	assertTaskLabelsEqualForTest(t, subTask.GetLabels(),
		[]string{"分组:工作", "我的标签", fmt.Sprintf("subscription:%d", restoredSub.ID)})

	var orphanTask model.Task
	if err := database.DB.Where("name = ?", "孤儿订阅任务").First(&orphanTask).Error; err != nil {
		t.Fatalf("load restored orphan task: %v", err)
	}
	assertTaskLabelsEqualForTest(t, orphanTask.GetLabels(), []string{"保留标签"})
}

// TestRestoreBackupManifestDropsSubscriptionLabelsWhenSubscriptionsNotSelected：
// 只勾选任务、不勾选订阅时映射为空，所有 subscription: 标签必须被丢弃（等价于修复前的行为），
// 但其它标签一个都不能少。
func TestRestoreBackupManifestDropsSubscriptionLabelsWhenSubscriptionsNotSelected(t *testing.T) {
	testutil.SetupTestEnv(t)

	manifest := BackupManifest{
		Format:    "daidai-panel-backup",
		Version:   "0.4.0",
		Source:    "daidai-panel",
		Selection: BackupSelection{Tasks: true},
		Data: BackupPayload{
			Tasks: []BackupTask{
				{
					Task: model.Task{
						Name:           "只恢复任务",
						Command:        "task only.py",
						CronExpression: "0 0 * * *",
						Status:         model.TaskStatusEnabled,
					},
					Labels: []string{"分组:工作", "subscription:7", "我的标签"},
				},
			},
			Subscriptions: []BackupSubscription{
				{
					Subscription: model.Subscription{ID: 7, Name: "不会被恢复的订阅", URL: "https://example.com/demo.git"},
				},
			},
		},
	}

	if err := restoreBackupManifest(manifest, t.TempDir()); err != nil {
		t.Fatalf("restore backup manifest: %v", err)
	}

	var task model.Task
	if err := database.DB.Where("name = ?", "只恢复任务").First(&task).Error; err != nil {
		t.Fatalf("load restored task: %v", err)
	}
	assertTaskLabelsEqualForTest(t, task.GetLabels(), []string{"分组:工作", "我的标签"})
}

// TestRestoreLegacyManifestWithoutLabelsKeyLeavesLabelsEmpty：
// 修复之前导出的备份里 tasks 根本没有 labels 键，恢复必须成功且标签为空 —— 行为与升级前逐字节一致。
func TestRestoreLegacyManifestWithoutLabelsKeyLeavesLabelsEmpty(t *testing.T) {
	testutil.SetupTestEnv(t)

	raw := `{
		"format": "daidai-panel-backup",
		"version": "0.4.0",
		"source": "daidai-panel",
		"selection": {"tasks": true, "subscriptions": true},
		"data": {
			"tasks": [
				{"id": 5, "name": "老备份任务", "command": "task legacy.py", "cron_expression": "0 0 * * *", "status": 1}
			],
			"subscriptions": [
				{"id": 3, "name": "老备份订阅", "type": "git-repo", "url": "https://example.com/legacy.git"}
			]
		}
	}`

	var manifest BackupManifest
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		t.Fatalf("decode legacy manifest: %v", err)
	}
	if err := restoreBackupManifest(manifest, t.TempDir()); err != nil {
		t.Fatalf("老备份必须能正常恢复，实际失败：%v", err)
	}

	var task model.Task
	if err := database.DB.Where("name = ?", "老备份任务").First(&task).Error; err != nil {
		t.Fatalf("load restored legacy task: %v", err)
	}
	if task.Labels != "" {
		t.Fatalf("老备份没有 labels 键，恢复后标签应为空，实际 %q", task.Labels)
	}

	var subscription model.Subscription
	if err := database.DB.Where("name = ?", "老备份订阅").First(&subscription).Error; err != nil {
		t.Fatalf("load restored legacy subscription: %v", err)
	}
	if subscription.AuthToken != "" {
		t.Fatalf("老备份没有 auth_token 键，恢复后应为空，实际 %q", subscription.AuthToken)
	}
}

// TestQingLongImportKeepsTaskLabels：青龙导入路径同样经过 BackupTask，标签不能在转换中丢掉。
func TestQingLongImportKeepsTaskLabels(t *testing.T) {
	testutil.SetupTestEnv(t)

	dbPath := filepath.Join(t.TempDir(), "qinglong.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE Crontabs (
			id INTEGER PRIMARY KEY,
			name TEXT,
			command TEXT,
			schedule TEXT,
			labels TEXT,
			isDisabled INTEGER
		);
		INSERT INTO Crontabs (id, name, command, schedule, labels, isDisabled)
		VALUES (1, '青龙任务', 'task demo.js', '0 0 * * *', '["京东","签到"]', 0);
	`); err != nil {
		db.Close()
		t.Fatalf("init qinglong crontabs: %v", err)
	}
	db.Close()

	manifest := BackupManifest{
		Format: "daidai-panel-backup",
		Source: "qinglong",
	}
	if err := enrichManifestFromQingLongDB(dbPath, &manifest); err != nil {
		t.Fatalf("enrich manifest from qinglong db: %v", err)
	}
	if len(manifest.Data.Tasks) != 1 {
		t.Fatalf("期望识别出 1 个青龙任务，实际 %d 个", len(manifest.Data.Tasks))
	}
	assertTaskLabelsEqualForTest(t, manifest.Data.Tasks[0].Labels, []string{"京东", "签到"})

	if err := restoreBackupManifest(manifest, t.TempDir()); err != nil {
		t.Fatalf("restore qinglong manifest: %v", err)
	}

	var task model.Task
	if err := database.DB.Where("name = ?", "青龙任务").First(&task).Error; err != nil {
		t.Fatalf("load imported qinglong task: %v", err)
	}
	if task.Labels != "京东,签到" {
		t.Fatalf("青龙导入的标签没落库，实际 %q", task.Labels)
	}
}

// readBackupManifestJSONForTest 从备份包（未加密的 .tgz）里取出 manifest.json 原始字节。
func readBackupManifestJSONForTest(t *testing.T, filePath string) []byte {
	t.Helper()

	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read backup file: %v", err)
	}
	gzipReader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("open gzip backup: %v", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar entry: %v", err)
		}
		if header.Name != "manifest.json" {
			continue
		}
		body, err := io.ReadAll(tarReader)
		if err != nil {
			t.Fatalf("read manifest body: %v", err)
		}
		return body
	}

	t.Fatal("备份包里没有 manifest.json")
	return nil
}

func assertTaskLabelsEqualForTest(t *testing.T, got, want []string) {
	t.Helper()
	// 标签顺序本身也是用户可见的（列表页按存储顺序渲染），所以按顺序比对。
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("标签不符：期望 %v，实际 %v", want, got)
	}
}
