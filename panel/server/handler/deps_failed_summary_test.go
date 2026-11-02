package handler_test

import (
	"net/http"
	"testing"

	"daidai-panel/database"
	"daidai-panel/handler"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

// newDepsFailedSummaryRouter 只挂依赖路由：newProtectedRouter() 里没有注册 DepsHandler，
// 这里不去动那个公共 helper，免得和其他用例互相影响。
func newDepsFailedSummaryRouter() *gin.Engine {
	engine := gin.New()
	handler.NewDepsHandler().RegisterRoutes(engine.Group("/api/v1"))
	return engine
}

// mustCreateFailedSummaryDep 造一条依赖行，失败时直接中断用例。
func mustCreateFailedSummaryDep(t *testing.T, depType, name, pythonVersion, status string) {
	t.Helper()

	dep := model.Dependency{Type: depType, Name: name, PythonVersion: pythonVersion, Status: status}
	if err := database.DB.Create(&dep).Error; err != nil {
		t.Fatalf("seed dependency %s/%s: %v", depType, name, err)
	}
}

// assertFailedByType 断言 failed_by_type 的形状与数字：三个键恒定存在、不多不少。
// 「键必须存在」这条是硬约束——前端直接渲染这三个数字，缺键会变成 undefined。
func assertFailedByType(t *testing.T, payload map[string]interface{}, wantNodeJS, wantPython, wantLinux float64) {
	t.Helper()

	summary, ok := payload["failed_by_type"].(map[string]interface{})
	if !ok {
		t.Fatalf("failed_by_type 应该是对象，实际 %#v", payload["failed_by_type"])
	}

	want := map[string]float64{
		model.DepTypeNodeJS: wantNodeJS,
		model.DepTypePython: wantPython,
		model.DepTypeLinux:  wantLinux,
	}
	if len(summary) != len(want) {
		t.Fatalf("failed_by_type 应当只有 nodejs/python/linux 三个键，实际 %#v", summary)
	}
	for key, expect := range want {
		got, exists := summary[key]
		if !exists {
			t.Fatalf("failed_by_type 缺少键 %q，前端会拿到 undefined：%#v", key, summary)
		}
		if got != expect {
			t.Fatalf("failed_by_type[%q] 期望 %v，实际 %#v", key, expect, got)
		}
	}
}

// assertDataOnlyContainsType 断言 data 仍然只含请求的那个类型，
// 反向守住「加了汇总之后原来的类型过滤还在」。
func assertDataOnlyContainsType(t *testing.T, payload map[string]interface{}, depType string, wantCount int) {
	t.Helper()

	rows, ok := payload["data"].([]interface{})
	if !ok {
		t.Fatalf("data 应该是数组，实际 %#v", payload["data"])
	}
	if len(rows) != wantCount {
		t.Fatalf("data 期望 %d 条，实际 %d 条：%#v", wantCount, len(rows), rows)
	}
	if total, exists := payload["total"]; !exists || total != float64(wantCount) {
		t.Fatalf("total 期望 %d，实际 %#v", wantCount, payload["total"])
	}
	for _, row := range rows {
		item, ok := row.(map[string]interface{})
		if !ok {
			t.Fatalf("data 元素应该是对象，实际 %#v", row)
		}
		if item["type"] != depType {
			t.Fatalf("data 里混进了非 %s 类型的依赖：%#v", depType, item)
		}
	}
}

// TestDepsListFailedSummaryCountsAllTypes 守住 GET /deps 新增的 failed_by_type：
// 它是跨类型的失败数汇总，三者之和必须等于侧栏角标（system_badges.go 的 deps_failed，
// 不分类型、不分 Python 版本的全表 status='failed' 计数），否则用户又会遇到
// 「角标 9、页面上任何地方都找不到 9」的问题。
func TestDepsListFailedSummaryCountsAllTypes(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newDepsFailedSummaryRouter()
	admin := testutil.MustCreateUser(t, "deps-summary-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
	auth := map[string]string{"Authorization": "Bearer " + token}

	// nodejs：2 失败 + 1 已安装 + 1 安装中
	mustCreateFailedSummaryDep(t, model.DepTypeNodeJS, "axios", "", model.DepStatusFailed)
	mustCreateFailedSummaryDep(t, model.DepTypeNodeJS, "dayjs", "", model.DepStatusFailed)
	mustCreateFailedSummaryDep(t, model.DepTypeNodeJS, "crypto-js", "", model.DepStatusInstalled)
	mustCreateFailedSummaryDep(t, model.DepTypeNodeJS, "got", "", model.DepStatusInstalling)

	// python：3.10 下 2 失败、3.11 下 3 失败，另有 1 条已安装的不该被算进去
	mustCreateFailedSummaryDep(t, model.DepTypePython, "requests", "3.10", model.DepStatusFailed)
	mustCreateFailedSummaryDep(t, model.DepTypePython, "httpx", "3.10", model.DepStatusFailed)
	mustCreateFailedSummaryDep(t, model.DepTypePython, "requests", "3.11", model.DepStatusFailed)
	mustCreateFailedSummaryDep(t, model.DepTypePython, "httpx", "3.11", model.DepStatusFailed)
	mustCreateFailedSummaryDep(t, model.DepTypePython, "pendulum", "3.11", model.DepStatusFailed)
	mustCreateFailedSummaryDep(t, model.DepTypePython, "numpy", "3.10", model.DepStatusInstalled)

	// linux：2 失败 + 1 卸载中
	mustCreateFailedSummaryDep(t, model.DepTypeLinux, "curl", "", model.DepStatusFailed)
	mustCreateFailedSummaryDep(t, model.DepTypeLinux, "git", "", model.DepStatusFailed)
	mustCreateFailedSummaryDep(t, model.DepTypeLinux, "vim", "", model.DepStatusRemoving)

	// 请求 nodejs：汇总里照样能看到 python / linux 的失败数
	nodeRec := performRequest(engine, http.MethodGet, "/api/v1/deps?type=nodejs", auth)
	if nodeRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", nodeRec.Code, nodeRec.Body.String())
	}
	nodePayload := decodeJSONMap(t, nodeRec)
	assertFailedByType(t, nodePayload, 2, 5, 2)
	assertDataOnlyContainsType(t, nodePayload, model.DepTypeNodeJS, 4)

	// 请求 linux：汇总与上面完全一致，不随请求的类型变化
	linuxRec := performRequest(engine, http.MethodGet, "/api/v1/deps?type=linux", auth)
	if linuxRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", linuxRec.Code, linuxRec.Body.String())
	}
	linuxPayload := decodeJSONMap(t, linuxRec)
	assertFailedByType(t, linuxPayload, 2, 5, 2)
	assertDataOnlyContainsType(t, linuxPayload, model.DepTypeLinux, 3)
}

// TestDepsListFailedSummaryPythonIgnoresVersionFilter 是本次最关键的一条：
// 请求只带 python_version=3.10 时，data 必须只剩 3.10 的行（原有过滤不能被破坏），
// 但 failed_by_type.python 仍然是跨所有版本的总数 —— 只有这样
// nodejs + python + linux 三者之和才等于侧栏角标那个不分版本的全表计数。
func TestDepsListFailedSummaryPythonIgnoresVersionFilter(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newDepsFailedSummaryRouter()
	admin := testutil.MustCreateUser(t, "deps-summary-py-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
	auth := map[string]string{"Authorization": "Bearer " + token}

	// 3.10 下 1 条失败、1 条已安装；3.11 下 2 条失败；3.12 下 1 条失败 => python 共 4 条失败
	mustCreateFailedSummaryDep(t, model.DepTypePython, "requests", "3.10", model.DepStatusFailed)
	mustCreateFailedSummaryDep(t, model.DepTypePython, "numpy", "3.10", model.DepStatusInstalled)
	mustCreateFailedSummaryDep(t, model.DepTypePython, "requests", "3.11", model.DepStatusFailed)
	mustCreateFailedSummaryDep(t, model.DepTypePython, "httpx", "3.11", model.DepStatusFailed)
	mustCreateFailedSummaryDep(t, model.DepTypePython, "requests", "3.12", model.DepStatusFailed)
	// 另外两个类型各 1 条失败，用来确认汇总确实是三类相加
	mustCreateFailedSummaryDep(t, model.DepTypeNodeJS, "axios", "", model.DepStatusFailed)
	mustCreateFailedSummaryDep(t, model.DepTypeLinux, "curl", "", model.DepStatusFailed)

	rec := performRequest(engine, http.MethodGet, "/api/v1/deps?type=python&python_version=3.10", auth)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	payload := decodeJSONMap(t, rec)

	// python 这一项若按 python_version=3.10 过滤会变成 1，那就和侧栏角标对不上了
	assertFailedByType(t, payload, 1, 4, 1)
	// data 仍然只有 3.10 的两条（1 失败 + 1 已安装）
	assertDataOnlyContainsType(t, payload, model.DepTypePython, 2)

	rows, _ := payload["data"].([]interface{})
	for _, row := range rows {
		item, _ := row.(map[string]interface{})
		if item["python_version"] != "3.10" {
			t.Fatalf("data 里混进了非 3.10 版本的 Python 依赖：%#v", item)
		}
	}
}

// TestDepsListFailedSummaryKeepsZeroKeys 守住「没有失败依赖时三个键仍然存在且为 0」。
// 用 map 直接省略空键的话，前端拿到的是 undefined，页签上会显示成空白而不是 0。
func TestDepsListFailedSummaryKeepsZeroKeys(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newDepsFailedSummaryRouter()
	admin := testutil.MustCreateUser(t, "deps-summary-zero-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
	auth := map[string]string{"Authorization": "Bearer " + token}

	// 只造非失败状态的依赖，确保汇总全为 0
	mustCreateFailedSummaryDep(t, model.DepTypeNodeJS, "axios", "", model.DepStatusInstalled)
	mustCreateFailedSummaryDep(t, model.DepTypePython, "requests", "3.10", model.DepStatusInstalling)
	mustCreateFailedSummaryDep(t, model.DepTypeLinux, "curl", "", model.DepStatusCancelled)

	rec := performRequest(engine, http.MethodGet, "/api/v1/deps?type=nodejs", auth)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	payload := decodeJSONMap(t, rec)

	assertFailedByType(t, payload, 0, 0, 0)
	assertDataOnlyContainsType(t, payload, model.DepTypeNodeJS, 1)
}
