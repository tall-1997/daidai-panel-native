package database_test

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// migrateEnvVarsCapturingStatements 对 env_vars 跑一次 AutoMigrate（生产启动、每条 ddp 命令走的同一个迁移器），
// 返回期间发出的全部 SQL，以及其中会改库的那部分（探测表结构的 SELECT / PRAGMA 不算写）。
func migrateEnvVarsCapturingStatements(t *testing.T) (all []string, writes []string) {
	t.Helper()

	recorder := &migrationSQLRecorder{Interface: logger.Discard}
	if err := database.DB.Session(&gorm.Session{Logger: recorder}).AutoMigrate(&model.EnvVar{}); err != nil {
		t.Fatalf("auto migrate env_vars: %v", err)
	}
	for _, statement := range recorder.statements {
		head := strings.ToUpper(strings.TrimSpace(statement))
		if strings.HasPrefix(head, "SELECT") || strings.HasPrefix(head, "PRAGMA") {
			continue
		}
		writes = append(writes, statement)
	}
	return recorder.statements, writes
}

// assertEnvVarsNotRebuilt 断言这次迁移没有整表重建：glebarez 的迁移器重建表就是「建 env_vars__temp → 拷数据 → 删原表 → 改名」。
func assertEnvVarsNotRebuilt(t *testing.T, stage string, all, writes []string) {
	t.Helper()

	for _, statement := range all {
		if strings.Contains(statement, "env_vars__temp") {
			t.Fatalf("%s：AutoMigrate 不应整表重建 env_vars，却发出了：%s", stage, statement)
		}
	}
	if len(writes) != 0 {
		t.Fatalf("%s：AutoMigrate 不应有任何写语句，实际：%v", stage, writes)
	}
}

// snapshotEnvVarRows 按存储原样取出每一行（quote() 保住 NULL 与浮点原文），用来证明迁移没动任何数据。
func snapshotEnvVarRows(t *testing.T) []string {
	t.Helper()

	var rows []string
	if err := database.DB.Raw(`SELECT quote(id) || '|' || quote(name) || '|' || quote(value) || '|' || quote(remarks) || '|' ||
		quote(enabled) || '|' || quote(position) || '|' || quote(sort_order) || '|' || quote("group") || '|' ||
		quote(created_at) || '|' || quote(updated_at) FROM env_vars ORDER BY id`).Scan(&rows).Error; err != nil {
		t.Fatalf("snapshot env_vars rows: %v", err)
	}
	return rows
}

func envVarsRootPage(t *testing.T) int64 {
	t.Helper()

	var rootPage int64
	if err := database.DB.Raw("SELECT rootpage FROM sqlite_master WHERE type = 'table' AND name = 'env_vars'").Scan(&rootPage).Error; err != nil {
		t.Fatalf("read env_vars rootpage: %v", err)
	}
	return rootPage
}

func envVarsSequence(t *testing.T) int64 {
	t.Helper()

	var seq int64
	if err := database.DB.Raw("SELECT seq FROM sqlite_sequence WHERE name = 'env_vars'").Scan(&seq).Error; err != nil {
		t.Fatalf("read env_vars sequence: %v", err)
	}
	return seq
}

// 契约 S3（#156）：Position 的 tag 是 default:10000，与 GORM 建表时写进 DDL 的 DEFAULT 10000 逐字一致，
// 之后每次 AutoMigrate（每次启动、每条 ddp 命令）都不再整表重建 env_vars。
// 以前 tag 写 10000.0，GORM 按字符串比较默认值永远对不上，每次都重建，还把自增序号重置成当前最大 id，
// 已删变量的 id 会被新变量复用（青龙 Open API、MCP 都按 id 改删环境变量）。
func TestEnvVarsAutoMigrateDoesNotRebuildTable(t *testing.T) {
	testutil.SetupTestEnv(t)

	for i := 1; i <= 10; i++ {
		env := model.EnvVar{Name: fmt.Sprintf("ENV_%d", i), Value: strings.Repeat("v", 50), Enabled: true, Position: float64(1000 * i)}
		if err := database.DB.Create(&env).Error; err != nil {
			t.Fatalf("create env %d: %v", i, err)
		}
	}
	if err := database.DB.Where("id > ?", 7).Delete(&model.EnvVar{}).Error; err != nil {
		t.Fatalf("delete tail env vars: %v", err)
	}

	rowsBefore := snapshotEnvVarRows(t)
	rootPageBefore := envVarsRootPage(t)
	if seq := envVarsSequence(t); seq != 10 {
		t.Fatalf("删掉 8~10 号后自增序号应仍是 10，实际 %d", seq)
	}

	// SetupTestEnv 已经跑过第一次 AutoMigrate，这里再连跑两次，模拟之后的两次重启。
	for round := 2; round <= 3; round++ {
		all, writes := migrateEnvVarsCapturingStatements(t)
		assertEnvVarsNotRebuilt(t, fmt.Sprintf("第 %d 次迁移", round), all, writes)
	}
	if rowsAfter := snapshotEnvVarRows(t); !reflect.DeepEqual(rowsAfter, rowsBefore) {
		t.Fatalf("迁移不应改动任何一行\nbefore=%v\nafter=%v", rowsBefore, rowsAfter)
	}
	if rootPage := envVarsRootPage(t); rootPage != rootPageBefore {
		t.Fatalf("env_vars 的根页从 %d 变成了 %d：迁移重建了整张表", rootPageBefore, rootPage)
	}

	// 不重建就不会重置自增序号：新变量拿 11，不会复用已删的 8。
	created := model.EnvVar{Name: "NEW_ENV", Enabled: true}
	if err := database.DB.Create(&created).Error; err != nil {
		t.Fatalf("create new env: %v", err)
	}
	if created.ID != 11 {
		t.Fatalf("新变量应拿到 id 11（不复用已删变量的 id），实际 %d", created.ID)
	}
	// 插入默认值不变：Position 给零值时仍落库默认的 10000。
	var reloaded model.EnvVar
	if err := database.DB.First(&reloaded, created.ID).Error; err != nil {
		t.Fatalf("reload new env: %v", err)
	}
	if reloaded.Position != 10000 {
		t.Fatalf("Position 零值应落库默认的 10000，实际 %v", reloaded.Position)
	}
}

// 存量库里 env_vars 的 DDL 若写着 DEFAULT 10000.0（老的补列写法），升级后最多再重建一次，之后稳定；数据原样保留。
func TestEnvVarsLegacyPositionDefaultSettlesAfterOneMigration(t *testing.T) {
	testutil.SetupTestEnv(t)

	legacyDDL := []string{
		"DROP TABLE env_vars",
		"CREATE TABLE `env_vars` (`id` integer PRIMARY KEY AUTOINCREMENT,`name` text NOT NULL,`value` text DEFAULT \"\"," +
			"`remarks` text DEFAULT \"\",`enabled` numeric DEFAULT true,`position` real DEFAULT 10000.0," +
			"`sort_order` integer DEFAULT 0,`group` text DEFAULT \"\",`created_at` datetime,`updated_at` datetime)",
		"CREATE INDEX `idx_env_vars_name` ON `env_vars`(`name`)",
		"CREATE INDEX `idx_env_vars_position` ON `env_vars`(`position`)",
		"CREATE INDEX `idx_env_vars_group` ON `env_vars`(`group`)",
	}
	for _, statement := range legacyDDL {
		if err := database.DB.Exec(statement).Error; err != nil {
			t.Fatalf("build legacy env_vars table: %s: %v", statement, err)
		}
	}
	for _, name := range []string{"LEGACY_A", "LEGACY_B", "LEGACY_C"} {
		// 老表没有 important 列（v3.3.6 / APP #16 才加）。GORM 插入时总会带上 tag 里有默认值的列（零值换成默认值写进 INSERT），
		// 往缺列的老表插 model 行会报 no column named important，这里要略过它。
		if err := database.DB.Omit("important", "secret", "sealed").Create(&model.EnvVar{Name: name, Value: name + "-value", Enabled: true}).Error; err != nil {
			t.Fatalf("create legacy env %s: %v", name, err)
		}
	}
	rowsBefore := snapshotEnvVarRows(t)

	// 第一次迁移允许重建一次（把 DDL 换成 DEFAULT 10000），之后就不该再动。
	migrateEnvVarsCapturingStatements(t)
	all, writes := migrateEnvVarsCapturingStatements(t)
	assertEnvVarsNotRebuilt(t, "老库升级后的第二次迁移", all, writes)
	if rowsAfter := snapshotEnvVarRows(t); !reflect.DeepEqual(rowsAfter, rowsBefore) {
		t.Fatalf("重建前后数据应原样保留\nbefore=%v\nafter=%v", rowsBefore, rowsAfter)
	}
}

// EnsureColumns 给老库补出来的 position 列，默认值必须与 model.EnvVar 的 tag 逐字一致，
// 否则补完列后的下一次 AutoMigrate 会因为默认值对不上再多重建一次整表。
func TestEnsureColumnsAddsEnvVarPositionDefaultMatchingModelTag(t *testing.T) {
	testutil.SetupTestEnv(t)

	// 更老的库：env_vars 还没有 position / sort_order / group 三列。
	for _, statement := range []string{
		"DROP TABLE env_vars",
		"CREATE TABLE `env_vars` (`id` integer PRIMARY KEY AUTOINCREMENT,`name` text NOT NULL,`value` text DEFAULT \"\"," +
			"`remarks` text DEFAULT \"\",`enabled` numeric DEFAULT true,`created_at` datetime,`updated_at` datetime)",
	} {
		if err := database.DB.Exec(statement).Error; err != nil {
			t.Fatalf("build older env_vars table: %s: %v", statement, err)
		}
	}
	database.EnsureColumns()

	var columnDefault string
	if err := database.DB.Raw("SELECT dflt_value FROM pragma_table_info('env_vars') WHERE name = 'position'").Scan(&columnDefault).Error; err != nil {
		t.Fatalf("read position column default: %v", err)
	}
	envVarSchema, err := schema.Parse(&model.EnvVar{}, &sync.Map{}, database.DB.NamingStrategy)
	if err != nil {
		t.Fatalf("parse model.EnvVar schema: %v", err)
	}
	tagDefault := envVarSchema.LookUpField("Position").DefaultValue
	if columnDefault != tagDefault {
		t.Fatalf("EnsureColumns 补列的默认值 %q 应与 model.EnvVar 的 tag 默认值 %q 一致", columnDefault, tagDefault)
	}
}
