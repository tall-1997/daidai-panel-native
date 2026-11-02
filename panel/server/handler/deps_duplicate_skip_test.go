package handler

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// nodejs / linux 依赖以前只有 Python 分支做「先查后插」，连点两次就会重复提交同一个包的安装。
// 这里守住补上的存在性检查：同名且已安装/安装中/排队中的直接跳过，不再重复建行。
func TestNodeAndLinuxDependencyCreateSkipsExistingName(t *testing.T) {
	for _, depType := range []string{model.DepTypeNodeJS, model.DepTypeLinux} {
		t.Run(depType, func(t *testing.T) {
			testutil.SetupTestEnv(t)

			originalRunner := dependencyInstallRunner
			defer func() {
				dependencyInstallRunner = originalRunner
			}()
			// 不真的去装包，只把状态推到 installed，模拟「第一次已经装好了」。
			dependencyInstallRunner = func(id uint, installType string, name string) {
				database.DB.Model(&model.Dependency{}).Where("id = ?", id).Update("status", model.DepStatusInstalled)
			}

			engine := newDepsTestRouter()
			token := testutil.MustCreateAccessToken(t, "admin", "admin")

			existing := model.Dependency{Type: depType, Name: "axios", Status: model.DepStatusInstalled}
			if err := database.DB.Create(&existing).Error; err != nil {
				t.Fatalf("seed dependency: %v", err)
			}

			rec := performDepsJSONRequest(engine, http.MethodPost, "/api/v1/deps", map[string]any{
				"type":  depType,
				"names": []string{"axios"},
			}, map[string]string{
				"Authorization": "Bearer " + token,
			})
			if rec.Code != http.StatusCreated {
				t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
			}

			var count int64
			database.DB.Model(&model.Dependency{}).
				Where("type = ? AND name = ?", depType, "axios").
				Count(&count)
			if count != 1 {
				t.Fatalf("同名依赖不应重复建行，期望 1 条，实际 %d 条", count)
			}
		})
	}
}

// 反向守住：同一个生态里不同名字的包必须照常建行，别把跳过逻辑写宽了。
func TestNodeDependencyCreateStillAddsDifferentName(t *testing.T) {
	testutil.SetupTestEnv(t)

	originalRunner := dependencyInstallRunner
	defer func() {
		dependencyInstallRunner = originalRunner
	}()
	dependencyInstallRunner = func(id uint, installType string, name string) {
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Update("status", model.DepStatusInstalled)
	}

	engine := newDepsTestRouter()
	token := testutil.MustCreateAccessToken(t, "admin", "admin")

	existing := model.Dependency{Type: model.DepTypeNodeJS, Name: "axios", Status: model.DepStatusInstalled}
	if err := database.DB.Create(&existing).Error; err != nil {
		t.Fatalf("seed dependency: %v", err)
	}

	rec := performDepsJSONRequest(engine, http.MethodPost, "/api/v1/deps", map[string]any{
		"type":  model.DepTypeNodeJS,
		"names": []string{"dayjs"},
	}, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var count int64
	database.DB.Model(&model.Dependency{}).Where("type = ?", model.DepTypeNodeJS).Count(&count)
	if count != 2 {
		t.Fatalf("不同名的包应正常新增，期望 2 条，实际 %d 条", count)
	}
}

// postPythonDependencyForExtrasTest 在单版本 3.12 镜像下提交一个 Python 依赖，runner 桩把每次安装的 (id, name)
// 推进 calls（先把状态改成 installed 再推，收到时数据库已经是终态）。
func postPythonDependencyForExtrasTest(t *testing.T, seed model.Dependency, name string) (string, chan [2]any) {
	t.Helper()
	testutil.SetupTestEnv(t)
	t.Setenv("DAIDAI_PYTHON_RUNTIME_MODE", "single")
	t.Setenv("DAIDAI_PYTHON_VERSION", "3.12")

	calls := make(chan [2]any, 4)
	originalRunner := dependencyInstallRunner
	t.Cleanup(func() {
		dependencyInstallRunner = originalRunner
	})
	dependencyInstallRunner = func(id uint, installType string, name string) {
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Update("status", model.DepStatusInstalled)
		calls <- [2]any{id, name}
	}

	if err := database.DB.Create(&seed).Error; err != nil {
		t.Fatalf("seed dependency: %v", err)
	}

	engine := newDepsTestRouter()
	token := testutil.MustCreateAccessToken(t, "admin", "admin")
	rec := performDepsJSONRequest(engine, http.MethodPost, "/api/v1/deps", map[string]any{
		"type":  model.DepTypePython,
		"names": []string{name},
	}, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String(), calls
}

// issue #166：已装 httpx==0.27.0 时手动新建 httpx[http2]，不能「已存在跳过」，
// 要把这条记录原地改名成合并后的 httpx[http2]==0.27.0 并重新安装 —— 仍是一包一条，不新建记录，
// 版本锁定也不能丢（记录名就是重建容器重装时用的写法）。
func TestPythonDependencyCreateUpgradesInstalledRecordForNewExtras(t *testing.T) {
	seed := model.Dependency{Type: model.DepTypePython, Name: "httpx==0.27.0", PythonVersion: "3.12", Status: model.DepStatusInstalled, Log: "old log"}
	body, calls := postPythonDependencyForExtrasTest(t, seed, "httpx[http2]")
	if strings.Contains(body, "已存在跳过") {
		t.Fatalf("httpx[http2] 不应被当成已存在跳过: %s", body)
	}

	select {
	case call := <-calls:
		if call[1] != "httpx[http2]==0.27.0" {
			t.Fatalf("expected reinstall with merged httpx[http2]==0.27.0, got %v", call[1])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expected dependency install runner to be called")
	}

	var stored []model.Dependency
	if err := database.DB.Where("type = ?", model.DepTypePython).Find(&stored).Error; err != nil {
		t.Fatalf("reload dependencies: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("应原地升级而不是新建记录，期望 1 条，实际 %+v", stored)
	}
	if stored[0].Name != "httpx[http2]==0.27.0" || stored[0].PythonVersion != "3.12" {
		t.Fatalf("expected record renamed to merged httpx[http2]==0.27.0 for 3.12, got %+v", stored[0])
	}
	if stored[0].Log == "old log" {
		t.Fatalf("expected old install log cleared on reinstall, got %q", stored[0].Log)
	}
}

// 反向守住：命中安装中的同名记录时照旧跳过，不改名、不重装，免得和正在进行的安装抢同一条记录。
func TestPythonDependencyCreateSkipsInstallingRecordEvenWithNewExtras(t *testing.T) {
	seed := model.Dependency{Type: model.DepTypePython, Name: "httpx", PythonVersion: "3.12", Status: model.DepStatusInstalling}
	body, calls := postPythonDependencyForExtrasTest(t, seed, "httpx[http2]")
	if !strings.Contains(body, "已存在跳过 1") {
		t.Fatalf("expected skipped message, got %s", body)
	}

	select {
	case call := <-calls:
		t.Fatalf("installing 记录不应触发重装，却调用了 runner: %v", call)
	case <-time.After(200 * time.Millisecond):
	}

	var stored []model.Dependency
	database.DB.Where("type = ?", model.DepTypePython).Find(&stored)
	if len(stored) != 1 || stored[0].Name != "httpx" || stored[0].Status != model.DepStatusInstalling {
		t.Fatalf("expected installing httpx record untouched, got %+v", stored)
	}
}

// 反向守住：已装的写法已经覆盖本次 extras（已装 httpx[http2,socks]，再提交 httpx[http2]）时照旧跳过。
func TestPythonDependencyCreateSkipsInstalledRecordWhenExtrasCovered(t *testing.T) {
	seed := model.Dependency{Type: model.DepTypePython, Name: "httpx[http2,socks]", PythonVersion: "3.12", Status: model.DepStatusInstalled}
	body, calls := postPythonDependencyForExtrasTest(t, seed, "httpx[http2]")
	if !strings.Contains(body, "已存在跳过 1") {
		t.Fatalf("expected skipped message, got %s", body)
	}

	select {
	case call := <-calls:
		t.Fatalf("extras 已覆盖时不应重装，却调用了 runner: %v", call)
	case <-time.After(200 * time.Millisecond):
	}

	var stored []model.Dependency
	database.DB.Where("type = ?", model.DepTypePython).Find(&stored)
	if len(stored) != 1 || stored[0].Name != "httpx[http2,socks]" {
		t.Fatalf("expected installed record untouched, got %+v", stored)
	}
}
