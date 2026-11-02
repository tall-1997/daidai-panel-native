package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"daidai-panel/service"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

// /system/info 要带面板版本号（#139）：开放 API / MCP 调用方靠它一次拿到对端版本，
// 而且字段要与资源快照平铺在同一层，不能包进新对象。
func TestSystemInfoIncludesPanelVersion(t *testing.T) {
	testutil.SetupTestEnv(t)

	// 真实资源采集会读磁盘 / 起外部命令，与本用例无关，换成固定值。
	previousResourceInfo := systemHealthGetResourceInfo
	systemHealthGetResourceInfo = func() service.ResourceInfo {
		return service.ResourceInfo{Hostname: "system-info-version-host"}
	}
	t.Cleanup(func() { systemHealthGetResourceInfo = previousResourceInfo })

	user := testutil.MustCreateUser(t, "system-info-version-viewer", "viewer")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	engine := gin.New()
	NewSystemHandler().RegisterRoutes(engine.Group("/api/v1"))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/info", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("system/info 应返回 200，实际 %d，body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析 system/info 响应失败: %v，body=%s", err, rec.Body.String())
	}
	if got, _ := payload.Data["version"].(string); got == "" || got != Version {
		t.Fatalf("system/info 的 version 应为面板版本 %q，实际 %#v", Version, payload.Data["version"])
	}
	if got, _ := payload.Data["hostname"].(string); got != "system-info-version-host" {
		t.Fatalf("资源快照字段应仍平铺在 data 下，hostname 实际 %#v", payload.Data["hostname"])
	}
}
