package database_test

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"gorm.io/gorm/schema"
)

// buildEnvVarsV335Table 把 env_vars 换成 v3.3.5 GORM 建出来的原样表（没有 important 列），并按 SQL 原样塞 3 行。
// 不能用 model.EnvVar 插：GORM 插入时总会带上 tag 里有默认值的列，往缺 important 列的老表插 model 行会报错。
func buildEnvVarsV335Table(t *testing.T) {
	t.Helper()

	for _, statement := range []string{
		"DROP TABLE env_vars",
		"CREATE TABLE `env_vars` (`id` integer PRIMARY KEY AUTOINCREMENT,`name` text NOT NULL,`value` text DEFAULT \"\"," +
			"`remarks` text DEFAULT \"\",`enabled` numeric DEFAULT true,`position` real DEFAULT 10000," +
			"`sort_order` integer DEFAULT 0,`group` text DEFAULT \"\",`created_at` datetime,`updated_at` datetime)",
		"CREATE INDEX `idx_env_vars_name` ON `env_vars`(`name`)",
		"CREATE INDEX `idx_env_vars_position` ON `env_vars`(`position`)",
		"CREATE INDEX `idx_env_vars_group` ON `env_vars`(`group`)",
		"INSERT INTO env_vars (name, value, enabled, position, sort_order, created_at, updated_at) VALUES " +
			"('KEEP_A', 'a', 1, 1000, 0, '2026-10-01 08:00:00', '2026-10-01 08:00:00')," +
			"('KEEP_B', 'b', 0, 2000, 0, '2026-10-01 08:00:00', '2026-10-01 08:00:00')," +
			"('KEEP_C', 'c', 1, 1000, 1, '2026-10-01 08:00:00', '2026-10-01 08:00:00')",
	} {
		if err := database.DB.Exec(statement).Error; err != nil {
			t.Fatalf("build v3.3.5 env_vars table: %s: %v", statement, err)
		}
	}
}

// APP #16：v3.3.5 老库升级按列补齐 env_vars（v3.4.0 含 important/secret/sealed，全部加列 ALTER，不整表重建），存量行全是 0，
// 之后再启动 0 写。整表重建会重置自增序号、让已删变量的 id 被复用（契约 S3）。
func TestEnvVarsImportantColumnUpgradeAddsOneColumnWithoutRebuild(t *testing.T) {
	testutil.SetupTestEnv(t)
	buildEnvVarsV335Table(t)
	rowsBefore := snapshotEnvVarRows(t)
	rootPageBefore := envVarsRootPage(t)

	all, writes := migrateEnvVarsCapturingStatements(t)
	for _, statement := range all {
		if strings.Contains(statement, "env_vars__temp") {
			t.Fatalf("升级不应整表重建 env_vars，却发出了：%s", statement)
		}
	}
	// v3.4.0 的 env_vars 比 v3.3.5 多三列（important + 加密变量的 secret / sealed），
	// 升级应当全是加列 ALTER，不整表重建；逐条确认真实需要的列都补上。
	sawImportant := false
	for _, write := range writes {
		if !strings.Contains(write, "ADD ") {
			t.Fatalf("升级只应发 ADD COLUMN，出现非加列写语句：%s", write)
		}
		if strings.Contains(write, "ADD `important` numeric DEFAULT false") {
			sawImportant = true
		}
	}
	if !sawImportant {
		t.Fatalf("升级应当补上 important 列，实际：%v", writes)
	}

	// 生产启动顺序：AutoMigrate → EnsureColumns（列已在，跳过）；之后再启动一次不该再写库。
	database.EnsureColumns()
	all, writes = migrateEnvVarsCapturingStatements(t)
	assertEnvVarsNotRebuilt(t, "补列之后的下一次启动", all, writes)

	if rowsAfter := snapshotEnvVarRows(t); !reflect.DeepEqual(rowsAfter, rowsBefore) {
		t.Fatalf("升级不应改动存量数据\nbefore=%v\nafter=%v", rowsBefore, rowsAfter)
	}
	if rootPage := envVarsRootPage(t); rootPage != rootPageBefore {
		t.Fatalf("env_vars 的根页从 %d 变成了 %d：升级重建了整张表", rootPageBefore, rootPage)
	}
	var importantCount int64
	if err := database.DB.Model(&model.EnvVar{}).Where("important = ?", true).Count(&importantCount).Error; err != nil {
		t.Fatalf("count important rows: %v", err)
	}
	if importantCount != 0 {
		t.Fatalf("存量行升级后应当全部不是重要变量，实际 %d 条", importantCount)
	}
}

// EnsureColumns 补出来的 important 列，默认值必须与 model.EnvVar 的 tag 逐字一致（契约 S3）。
// 生产顺序是 AutoMigrate 先跑，正常走不到这条补列；它防的是以后有人调换顺序或单跑 EnsureColumns，
// 补完列后的下一次 AutoMigrate 也不能因此整表重建。
func TestEnsureColumnsAddsEnvVarImportantDefaultMatchingModelTag(t *testing.T) {
	testutil.SetupTestEnv(t)
	buildEnvVarsV335Table(t)

	database.EnsureColumns()

	var columnDefault string
	if err := database.DB.Raw("SELECT dflt_value FROM pragma_table_info('env_vars') WHERE name = 'important'").Scan(&columnDefault).Error; err != nil {
		t.Fatalf("read important column default: %v", err)
	}
	envVarSchema, err := schema.Parse(&model.EnvVar{}, &sync.Map{}, database.DB.NamingStrategy)
	if err != nil {
		t.Fatalf("parse model.EnvVar schema: %v", err)
	}
	if tagDefault := envVarSchema.LookUpField("Important").DefaultValue; columnDefault != tagDefault {
		t.Fatalf("EnsureColumns 补列的默认值 %q 应与 model.EnvVar 的 tag 默认值 %q 一致", columnDefault, tagDefault)
	}

	all, writes := migrateEnvVarsCapturingStatements(t)
	assertEnvVarsNotRebuilt(t, "EnsureColumns 补列之后的 AutoMigrate", all, writes)
}
