package database_test

import (
	"fmt"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// uniqueNameTargetFixture 描述一张目标表在测试里怎么摘索引、怎么造数、怎么把索引重建回来。
//
// 之所以做成「按表名索引的 fixture 表 + 遍历 database.UniqueNameTargets」，
// 而不是给每张表手写一个用例：漏测的代价太大。
// database.UniqueNameTargets 里的表名一旦写错（或将来 model 改了 TableName），
// 去重会静默跳过那张表，老库升级时 AutoMigrate 建唯一索引失败直接 log.Fatalf、面板起不来，
// 而 go test 依旧全绿。往 UniqueNameTargets 加表却忘了在这里补 fixture，用例会立刻失败。
type uniqueNameTargetFixture struct {
	// dest 是这张表对应的 GORM model，用来摘索引、造数和把唯一索引重建回来。
	dest interface{}
	// dropIndex 传给 Migrator().DropIndex，GORM 既认索引名也认字段名；
	// 这里对单列索引传字段名，免得索引命名规则一变测试就红。
	dropIndex string
	// seed 往表里插一条叫 name 的记录；index 用来给「除 name 外的其它唯一列」错开取值
	// （典型是 open_apps.app_key，它自己也是唯一索引，不错开就会撞到另一个约束上去）。
	seed func(t *testing.T, name string, index int)
}

// uniqueNameTargetFixtures 必须在 testutil.SetupTestEnv 之后调用：
// 里面的闭包会持有当次测试库的行 ID，跨用例复用会拿到上一轮已经被销毁的库里的 ID。
func uniqueNameTargetFixtures(t *testing.T) map[string]uniqueNameTargetFixture {
	t.Helper()

	// platform_tokens 上有指向 platforms 的外键，且 PRAGMA foreign_keys=ON，
	// 所以造令牌前必须先有一个真实平台；两条重名令牌还必须挂在同一个平台下才构成冲突。
	var platformID uint

	return map[string]uniqueNameTargetFixture{
		"notify_channels": {
			dest:      &model.NotifyChannel{},
			dropIndex: "Name",
			seed: func(t *testing.T, name string, index int) {
				t.Helper()
				seedNotifyChannel(t, name)
			},
		},
		"ssh_keys": {
			dest:      &model.SSHKey{},
			dropIndex: "Name",
			seed: func(t *testing.T, name string, index int) {
				t.Helper()
				key := model.SSHKey{Name: name, PrivateKey: fmt.Sprintf("-----BEGIN KEY %d-----", index)}
				if err := database.DB.Create(&key).Error; err != nil {
					t.Fatalf("seed ssh key %q: %v", name, err)
				}
			},
		},
		"task_views": {
			dest:      &model.TaskView{},
			dropIndex: "Name",
			seed: func(t *testing.T, name string, index int) {
				t.Helper()
				view := model.TaskView{Name: name, SortOrder: index}
				if err := database.DB.Create(&view).Error; err != nil {
					t.Fatalf("seed task view %q: %v", name, err)
				}
			},
		},
		"open_apps": {
			dest:      &model.OpenApp{},
			dropIndex: "Name",
			seed: func(t *testing.T, name string, index int) {
				t.Helper()
				// app_key 本身也是唯一索引，两条同名应用必须把它错开，
				// 否则挡下第二条的是 app_key 约束而不是我们想测的 name 约束。
				app := model.OpenApp{
					Name:      name,
					AppKey:    fmt.Sprintf("app-key-%d", index),
					AppSecret: fmt.Sprintf("app-secret-%d", index),
					Scopes:    "tasks",
					Enabled:   true,
				}
				if err := database.DB.Create(&app).Error; err != nil {
					t.Fatalf("seed open app %q: %v", name, err)
				}
			},
		},
		"platform_tokens": {
			dest:      &model.PlatformToken{},
			dropIndex: "idx_platform_tokens_platform_name",
			seed: func(t *testing.T, name string, index int) {
				t.Helper()
				if platformID == 0 {
					platform := model.Platform{Name: "jd", Label: "京东"}
					if err := database.DB.Create(&platform).Error; err != nil {
						t.Fatalf("seed platform for token fixture: %v", err)
					}
					platformID = platform.ID
				}
				token := model.PlatformToken{
					PlatformID: platformID,
					Name:       name,
					Token:      fmt.Sprintf("token-%d", index),
					Enabled:    true,
				}
				if err := database.DB.Create(&token).Error; err != nil {
					t.Fatalf("seed platform token %q: %v", name, err)
				}
			},
		},
	}
}

// mustUniqueNameTargetFixture 取出目标表的 fixture，取不到就直接失败。
// 这条失败信息就是「新增唯一索引表时必须一并补测试」的强制口径。
func mustUniqueNameTargetFixture(t *testing.T, table string) uniqueNameTargetFixture {
	t.Helper()

	fixture, ok := uniqueNameTargetFixtures(t)[table]
	if !ok {
		t.Fatalf("database.UniqueNameTargets 里的表 %q 没有对应的测试造数逻辑，"+
			"新增名称唯一索引时必须同步在 uniqueNameTargetFixtures 里补一条，否则这张表完全没有覆盖", table)
	}
	return fixture
}

// dropNameUniqueIndexes 把本轮新加的名称唯一索引先摘掉，用来模拟「升级前的老库」。
// 只有摘掉索引才能造出同名数据，再回过头验证 DeduplicateBeforeUniqueIndex 能不能把它救回来。
func dropNameUniqueIndexes(t *testing.T) {
	t.Helper()

	for _, target := range database.UniqueNameTargets {
		fixture := mustUniqueNameTargetFixture(t, target.Table)
		if err := database.DB.Migrator().DropIndex(fixture.dest, fixture.dropIndex); err != nil {
			t.Fatalf("drop unique index %q on %s: %v", fixture.dropIndex, target.Table, err)
		}
	}
}

func seedNotifyChannel(t *testing.T, name string) uint {
	t.Helper()

	ch := model.NotifyChannel{
		Name:      name,
		Type:      "webhook",
		Config:    `{"url":"https://example.com/webhook"}`,
		PushScope: model.NotifyPushScopeDefault,
		Enabled:   true,
	}
	if err := database.DB.Create(&ch).Error; err != nil {
		t.Fatalf("seed notify channel %q: %v", name, err)
	}
	return ch.ID
}

func loadNotifyChannelNames(t *testing.T) []string {
	t.Helper()

	var channels []model.NotifyChannel
	if err := database.DB.Order("id ASC").Find(&channels).Error; err != nil {
		t.Fatalf("load notify channels: %v", err)
	}
	names := make([]string, len(channels))
	for i, ch := range channels {
		names[i] = ch.Name
	}
	return names
}

// loadNamesOrdered 用裸 SQL 按 id 顺序读名称，这样同一段断言能套用到任意一张目标表上。
func loadNamesOrdered(t *testing.T, table string) []string {
	t.Helper()

	type nameRow struct {
		Name string
	}
	var rows []nameRow
	if err := database.DB.Raw("SELECT name AS name FROM " + table + " ORDER BY id ASC").Scan(&rows).Error; err != nil {
		t.Fatalf("load names from %s: %v", table, err)
	}
	names := make([]string, len(rows))
	for i, row := range rows {
		names[i] = row.Name
	}
	return names
}

// 表驱动遍历 database.UniqueNameTargets：每张目标表都要真的走一遍
// 「造重名 -> 去重改名 -> 唯一索引能建出来」，而不是只测其中两张。
// 以前 ssh_keys / task_views / open_apps 的表名字符串从没被任何测试触达过。
func TestDeduplicateBeforeUniqueIndexCoversEveryUniqueNameTarget(t *testing.T) {
	if len(database.UniqueNameTargets) == 0 {
		t.Fatal("database.UniqueNameTargets 是空的，去重迁移等于没跑")
	}

	for _, target := range database.UniqueNameTargets {
		target := target
		t.Run(target.Table, func(t *testing.T) {
			testutil.SetupTestEnv(t)
			fixture := mustUniqueNameTargetFixture(t, target.Table)

			// 表名写错是这里最致命也最静默的失败模式，先把它钉死。
			// 除了这条存在性断言，下面「用 model 造数、用 target.Table 裸 SQL 读回来」这一对
			// 本身就是表名一致性的行为断言：表名对不上时读回来的是 0 条，用例必红。
			if !database.DB.Migrator().HasTable(target.Table) {
				t.Fatalf("表 %q 在测试库里不存在（表名写错？），去重迁移会静默跳过它", target.Table)
			}
			// 隔离列同理：列名写错时 DeduplicateBeforeUniqueIndex 会因为缺列直接 continue，同样是静默失效。
			if target.ScopeColumn != "" && !database.DB.Migrator().HasColumn(fixture.dest, target.ScopeColumn) {
				t.Fatalf("表 %q 上没有隔离列 %q，去重迁移会静默跳过它", target.Table, target.ScopeColumn)
			}

			// 摘掉唯一索引，模拟升级前的老库，才能造出重名数据。
			dropNameUniqueIndexes(t)
			fixture.seed(t, "重名", 1)
			fixture.seed(t, "重名", 2)
			fixture.seed(t, "独立名", 3)

			database.DeduplicateBeforeUniqueIndex()

			names := loadNamesOrdered(t, target.Table)
			want := []string{"重名", "重名 (2)", "独立名"}
			if len(names) != len(want) {
				t.Fatalf("%s：去重绝不能删数据，期望仍有 %d 条，实际 %d 条：%v", target.Table, len(want), len(names), names)
			}
			for i, expected := range want {
				if names[i] != expected {
					t.Fatalf("%s：第 %d 条名称不符，期望 %q 实际 %q（全部=%v）", target.Table, i, expected, names[i], names)
				}
			}

			// 改完名之后唯一索引必须能建出来，否则线上就是 AutoMigrate 直接 log.Fatalf。
			if err := database.DB.AutoMigrate(fixture.dest); err != nil {
				t.Fatalf("%s：去重后重建唯一索引仍然失败：%v", target.Table, err)
			}
		})
	}
}

// 核心用例：老库里的重名记录必须被「改名」而不是被删掉，
// 且新名字要避开库里已经存在的 "xxx (2)"，改完之后唯一索引能顺利建出来。
func TestDeduplicateBeforeUniqueIndexRenamesInsteadOfDeleting(t *testing.T) {
	testutil.SetupTestEnv(t)
	dropNameUniqueIndexes(t)

	// 故意把「已经叫 渠道A (2) 的老记录」插在中间：直接拼 (2) 会撞上它。
	seedNotifyChannel(t, "渠道A")
	seedNotifyChannel(t, "渠道A (2)")
	seedNotifyChannel(t, "渠道A")
	seedNotifyChannel(t, "渠道A")
	seedNotifyChannel(t, "渠道B")

	database.DeduplicateBeforeUniqueIndex()

	names := loadNotifyChannelNames(t)
	if len(names) != 5 {
		t.Fatalf("去重迁移绝不能删数据，期望仍有 5 条，实际 %d 条：%v", len(names), names)
	}
	want := []string{"渠道A", "渠道A (2)", "渠道A (3)", "渠道A (4)", "渠道B"}
	for i, expected := range want {
		if names[i] != expected {
			t.Fatalf("第 %d 条名称不符：期望 %q，实际 %q（全部=%v）", i, expected, names[i], names)
		}
	}

	// 改完名之后唯一索引必须能建出来，否则线上就是 AutoMigrate 直接 log.Fatalf。
	if err := database.DB.AutoMigrate(&model.NotifyChannel{}); err != nil {
		t.Fatalf("去重后重建唯一索引仍然失败：%v", err)
	}
}

// NextAvailableName 是启动迁移与「恢复备份」两条路径共用的改名规则，
// 单独钉住它的行为，避免哪天有人为了某一条路径的方便改坏另一条。
func TestNextAvailableNameSkipsOccupiedCandidates(t *testing.T) {
	// 没被占用时原样返回：恢复备份里绝大多数记录走的是这条路，不能凭空加后缀。
	if got := database.NextAvailableName(map[string]bool{}, "渠道A"); got != "渠道A" {
		t.Fatalf("名字没被占用时应原样返回，实际 %q", got)
	}

	// 被占用时从 (2) 开始，并跳过同样被占用的候选。
	used := map[string]bool{"渠道A": true, "渠道A (2)": true, "渠道A (3)": true}
	if got := database.NextAvailableName(used, "渠道A"); got != "渠道A (4)" {
		t.Fatalf("应跳过已被占用的候选名，期望 %q，实际 %q", "渠道A (4)", got)
	}

	// nil map 等价于「什么都没被占用」，调用方不需要额外判空。
	if got := database.NextAvailableName(nil, "渠道A"); got != "渠道A" {
		t.Fatalf("nil map 应视为无占用，实际 %q", got)
	}
}

// 幂等：重复启动不能反复改名，否则用户每升级一次名字就多一个后缀。
func TestDeduplicateBeforeUniqueIndexIsIdempotent(t *testing.T) {
	testutil.SetupTestEnv(t)
	dropNameUniqueIndexes(t)

	seedNotifyChannel(t, "渠道A")
	seedNotifyChannel(t, "渠道A")
	seedNotifyChannel(t, "渠道B")

	database.DeduplicateBeforeUniqueIndex()
	first := loadNotifyChannelNames(t)

	database.DeduplicateBeforeUniqueIndex()
	database.DeduplicateBeforeUniqueIndex()
	second := loadNotifyChannelNames(t)

	if len(first) != len(second) {
		t.Fatalf("重复执行改变了行数：第一次 %v，之后 %v", first, second)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("重复执行又改了一次名：第一次 %v，之后 %v", first, second)
		}
	}
}

// 没有重名时什么都不该动（也是幂等的另一面）。
func TestDeduplicateBeforeUniqueIndexLeavesCleanDataUntouched(t *testing.T) {
	testutil.SetupTestEnv(t)
	dropNameUniqueIndexes(t)

	seedNotifyChannel(t, "渠道A")
	seedNotifyChannel(t, "渠道B")
	seedNotifyChannel(t, "渠道C")

	database.DeduplicateBeforeUniqueIndex()

	names := loadNotifyChannelNames(t)
	want := []string{"渠道A", "渠道B", "渠道C"}
	for i, expected := range want {
		if names[i] != expected {
			t.Fatalf("干净数据被误改：期望 %v，实际 %v", want, names)
		}
	}
}

// 平台令牌的唯一键是「平台 + 名称」：不同平台下的同名令牌是合法用法，一条都不能动。
func TestDeduplicateBeforeUniqueIndexScopesPlatformTokensByPlatform(t *testing.T) {
	testutil.SetupTestEnv(t)
	dropNameUniqueIndexes(t)

	// platform_tokens 上有指向 platforms 的外键，且 PRAGMA foreign_keys=ON，所以先建真实平台。
	seedPlatform := func(name string) uint {
		t.Helper()
		platform := model.Platform{Name: name, Label: name}
		if err := database.DB.Create(&platform).Error; err != nil {
			t.Fatalf("seed platform %q: %v", name, err)
		}
		return platform.ID
	}
	jd := seedPlatform("jd")
	taobao := seedPlatform("taobao")

	seedToken := func(platformID uint, name string) {
		t.Helper()
		token := model.PlatformToken{
			PlatformID: platformID,
			Name:       name,
			Token:      "token-" + name,
			Enabled:    true,
		}
		if err := database.DB.Create(&token).Error; err != nil {
			t.Fatalf("seed platform token %q: %v", name, err)
		}
	}

	seedToken(jd, "主号")
	seedToken(taobao, "主号")
	seedToken(jd, "主号")

	database.DeduplicateBeforeUniqueIndex()

	var tokens []model.PlatformToken
	if err := database.DB.Order("id ASC").Find(&tokens).Error; err != nil {
		t.Fatalf("load platform tokens: %v", err)
	}
	if len(tokens) != 3 {
		t.Fatalf("期望仍有 3 条令牌，实际 %d 条", len(tokens))
	}
	if tokens[0].Name != "主号" {
		t.Fatalf("同平台第一条应保留原名，实际 %q", tokens[0].Name)
	}
	if tokens[1].PlatformID != taobao || tokens[1].Name != "主号" {
		t.Fatalf("另一个平台下的同名令牌不该被改名，实际 platform_id=%d name=%q", tokens[1].PlatformID, tokens[1].Name)
	}
	if tokens[2].Name != "主号 (2)" {
		t.Fatalf("同平台下的重名令牌应改成「主号 (2)」，实际 %q", tokens[2].Name)
	}

	if err := database.DB.AutoMigrate(&model.PlatformToken{}); err != nil {
		t.Fatalf("去重后重建平台令牌唯一索引仍然失败：%v", err)
	}
}

// 全新库（表都还没建出来）时必须安全跳过：这个函数跑在 AutoMigrate 之前，第一次启动一定是这种形态。
func TestDeduplicateBeforeUniqueIndexSkipsMissingTables(t *testing.T) {
	testutil.SetupTestEnv(t)

	// 目标表清单同样从 UniqueNameTargets 推出来，新增表时这条用例自动覆盖到。
	targets := make([]interface{}, 0, len(database.UniqueNameTargets))
	for _, target := range database.UniqueNameTargets {
		targets = append(targets, mustUniqueNameTargetFixture(t, target.Table).dest)
	}

	if err := database.DB.Migrator().DropTable(targets...); err != nil {
		t.Fatalf("drop tables to simulate fresh database: %v", err)
	}

	// 表不存在时不能报错、不能 panic，直接静默跳过。
	database.DeduplicateBeforeUniqueIndex()

	// 跳过之后连接仍然可用，AutoMigrate 能把表正常建回来。
	if err := database.DB.AutoMigrate(targets...); err != nil {
		t.Fatalf("跳过后重新建表失败：%v", err)
	}

	var count int64
	if err := database.DB.Model(&model.NotifyChannel{}).Count(&count).Error; err != nil {
		t.Fatalf("count notify channels: %v", err)
	}
	if count != 0 {
		t.Fatalf("新建的空表应该是 0 条，实际 %d 条", count)
	}
}
