package database_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestCloseCheckpointsWALAndRemovesSidecarFiles 锁住面板关停最后一步 database.Close 的效果：
// SQLite 在最后一个连接关闭时做 checkpoint 并删掉 -wal / -shm，数据目录里只剩 daidai.db。
// 原来面板从不关库，被停掉后数据几乎全在 -wal 里（实测 db 只有 4 KB、wal 1.8 MB），
// 只拷 daidai.db 的备份方式会丢数据，下次启动还要回放 WAL。
func TestCloseCheckpointsWALAndRemovesSidecarFiles(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	dbPath := filepath.Join(root, "test.db")

	if err := database.DB.Create(&model.SystemConfig{Key: "close-checkpoint-probe", Value: "kept"}).Error; err != nil {
		t.Fatalf("write probe row: %v", err)
	}
	// 前提：WAL 模式下刚写的数据还在 -wal 里。这一条不成立的话，下面「-wal 消失」的断言就没有区分度。
	if _, err := os.Stat(dbPath + "-wal"); err != nil {
		t.Fatalf("expected %s-wal to exist before Close (journal_mode=WAL), got %v", dbPath, err)
	}

	if err := database.Close(time.Second); err != nil {
		t.Fatalf("database.Close: %v", err)
	}

	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(dbPath + suffix); !os.IsNotExist(err) {
			t.Fatalf("expected %s%s to be removed after Close (checkpoint on last connection close), stat err=%v", dbPath, suffix, err)
		}
	}

	// checkpoint 之后数据在 daidai.db 本体里：换一个全新的连接也读得到。
	reopened, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	sqlDB, err := reopened.DB()
	if err != nil {
		t.Fatalf("get reopened sql.DB: %v", err)
	}
	defer sqlDB.Close()

	var row model.SystemConfig
	if err := reopened.Where("key = ?", "close-checkpoint-probe").First(&row).Error; err != nil {
		t.Fatalf("read probe row from reopened database: %v", err)
	}
	if row.Value != "kept" {
		t.Fatalf("expected probe value %q after reopen, got %q", "kept", row.Value)
	}
}
