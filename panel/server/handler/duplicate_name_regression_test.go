package handler_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"daidai-panel/database"
	"daidai-panel/handler"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

// 这一组用例守住 issue #105 的后端兜底：前端按钮锁挡不住「30s 超时后用户手动再点一次」，
// 语义唯一的资源必须在 DB 层拦住第二次创建，并且回给用户一句能看懂的中文，而不是 500 或者静默成功。

func postJSON(t *testing.T, engine *gin.Engine, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func assertDuplicateRejected(t *testing.T, rec *httptest.ResponseRecorder, wantMessage string) {
	t.Helper()

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("重复创建应返回 400，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	payload := decodeJSONMap(t, rec)
	got, _ := payload["error"].(string)
	if got != wantMessage {
		t.Fatalf("重复创建的提示文案不符：期望 %q，实际 %q", wantMessage, got)
	}
}

func TestCreateNotifyChannelRejectsDuplicateName(t *testing.T) {
	testutil.SetupTestEnv(t)

	admin := testutil.MustCreateUser(t, "dup-notify-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
	engine := newProtectedRouter()

	body := `{"name":"渠道A","type":"webhook","config":"{\"url\":\"https://example.com/webhook\"}"}`
	if rec := postJSON(t, engine, "/api/v1/notifications", body, token); rec.Code != http.StatusCreated {
		t.Fatalf("首次创建应成功，实际 %d，body=%s", rec.Code, rec.Body.String())
	}

	assertDuplicateRejected(t, postJSON(t, engine, "/api/v1/notifications", body, token), "同名通知渠道已存在")

	var count int64
	database.DB.Model(&model.NotifyChannel{}).Where("name = ?", "渠道A").Count(&count)
	if count != 1 {
		t.Fatalf("被拒绝的请求不应落库，期望 1 条，实际 %d 条", count)
	}
}

func TestCreateTaskViewRejectsDuplicateName(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "dup-view-operator", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()

	body := `{"name":"我的视图"}`
	if rec := postJSON(t, engine, "/api/v1/tasks/views", body, token); rec.Code != http.StatusOK {
		t.Fatalf("首次创建应成功，实际 %d，body=%s", rec.Code, rec.Body.String())
	}

	assertDuplicateRejected(t, postJSON(t, engine, "/api/v1/tasks/views", body, token), "同名任务视图已存在")

	var count int64
	database.DB.Model(&model.TaskView{}).Where("name = ?", "我的视图").Count(&count)
	if count != 1 {
		t.Fatalf("被拒绝的请求不应落库，期望 1 条，实际 %d 条", count)
	}
}

func TestCreateSSHKeyRejectsDuplicateName(t *testing.T) {
	testutil.SetupTestEnv(t)

	admin := testutil.MustCreateUser(t, "dup-ssh-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)

	engine := gin.New()
	handler.NewSSHKeyHandler().RegisterRoutes(engine.Group("/api/v1"))

	body := `{"name":"我的密钥","private_key":"-----BEGIN KEY-----"}`
	if rec := postJSON(t, engine, "/api/v1/ssh-keys", body, token); rec.Code != http.StatusCreated {
		t.Fatalf("首次创建应成功，实际 %d，body=%s", rec.Code, rec.Body.String())
	}

	assertDuplicateRejected(t, postJSON(t, engine, "/api/v1/ssh-keys", body, token), "同名 SSH 密钥已存在")

	var count int64
	database.DB.Model(&model.SSHKey{}).Where("name = ?", "我的密钥").Count(&count)
	if count != 1 {
		t.Fatalf("被拒绝的请求不应落库，期望 1 条，实际 %d 条", count)
	}
}

func TestCreateOpenAppRejectsDuplicateName(t *testing.T) {
	testutil.SetupTestEnv(t)

	admin := testutil.MustCreateUser(t, "dup-app-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)

	engine := gin.New()
	handler.NewOpenAPIHandler().RegisterRoutes(engine.Group("/api/v1"))

	body := `{"name":"我的应用","scopes":"tasks"}`
	if rec := postJSON(t, engine, "/api/v1/open-api/apps", body, token); rec.Code != http.StatusCreated {
		t.Fatalf("首次创建应成功，实际 %d，body=%s", rec.Code, rec.Body.String())
	}

	assertDuplicateRejected(t, postJSON(t, engine, "/api/v1/open-api/apps", body, token), "同名应用已存在")

	var count int64
	database.DB.Model(&model.OpenApp{}).Where("name = ?", "我的应用").Count(&count)
	if count != 1 {
		t.Fatalf("被拒绝的请求不应落库，期望 1 条，实际 %d 条", count)
	}
}

// 平台令牌的唯一键是「平台 + 名称」：同平台重名要拦，跨平台同名必须放行。
func TestCreatePlatformTokenRejectsDuplicateOnlyWithinSamePlatform(t *testing.T) {
	testutil.SetupTestEnv(t)

	admin := testutil.MustCreateUser(t, "dup-token-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)

	engine := gin.New()
	handler.NewPlatformTokenHandler().RegisterRoutes(engine.Group("/api/v1"))

	jd := model.Platform{Name: "jd", Label: "京东"}
	if err := database.DB.Create(&jd).Error; err != nil {
		t.Fatalf("seed platform jd: %v", err)
	}
	taobao := model.Platform{Name: "taobao", Label: "淘宝"}
	if err := database.DB.Create(&taobao).Error; err != nil {
		t.Fatalf("seed platform taobao: %v", err)
	}

	jdBody := fmt.Sprintf(`{"platform_id":%d,"name":"主号","token":"jd-token"}`, jd.ID)
	if rec := postJSON(t, engine, "/api/v1/platform-tokens", jdBody, token); rec.Code != http.StatusCreated {
		t.Fatalf("首次创建应成功，实际 %d，body=%s", rec.Code, rec.Body.String())
	}

	assertDuplicateRejected(t, postJSON(t, engine, "/api/v1/platform-tokens", jdBody, token), "该平台下已存在同名令牌")

	// 换一个平台、同一个名字，属于正常用法，必须放行。
	taobaoBody := fmt.Sprintf(`{"platform_id":%d,"name":"主号","token":"taobao-token"}`, taobao.ID)
	if rec := postJSON(t, engine, "/api/v1/platform-tokens", taobaoBody, token); rec.Code != http.StatusCreated {
		t.Fatalf("不同平台下的同名令牌应放行，实际 %d，body=%s", rec.Code, rec.Body.String())
	}

	var count int64
	database.DB.Model(&model.PlatformToken{}).Where("name = ?", "主号").Count(&count)
	if count != 2 {
		t.Fatalf("期望两个平台各一条令牌，实际 %d 条", count)
	}
}

// 订阅刻意不加 DB 唯一索引，只做「先查后插」：
// 地址 + 分支 + 子目录 + 保存目录 + 别名五者全同才算重复；换个分支订阅同一个仓库是合法用法，必须放行。
func TestCreateSubscriptionRejectsDuplicateOnlyWhenBranchAndPathMatch(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "dup-sub-operator", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()

	body := `{"name":"仓库A","type":"git-repo","url":"https://github.com/example/a.git","branch":"main"}`
	if rec := postJSON(t, engine, "/api/v1/subscriptions", body, token); rec.Code != http.StatusCreated {
		t.Fatalf("首次创建应成功，实际 %d，body=%s", rec.Code, rec.Body.String())
	}

	dupBody := `{"name":"仓库A 再来一次","type":"git-repo","url":"https://github.com/example/a.git","branch":"main"}`
	assertDuplicateRejected(t, postJSON(t, engine, "/api/v1/subscriptions", dupBody, token), "相同地址、分支、子目录、保存目录和别名的订阅已存在")

	// 同一个仓库换个分支是合法场景，不能被误伤。
	otherBranch := `{"name":"仓库A dev","type":"git-repo","url":"https://github.com/example/a.git","branch":"dev"}`
	if rec := postJSON(t, engine, "/api/v1/subscriptions", otherBranch, token); rec.Code != http.StatusCreated {
		t.Fatalf("同仓库不同分支应放行，实际 %d，body=%s", rec.Code, rec.Body.String())
	}

	var count int64
	database.DB.Model(&model.Subscription{}).Where("url = ?", "https://github.com/example/a.git").Count(&count)
	if count != 2 {
		t.Fatalf("期望两条订阅（main 与 dev），实际 %d 条", count)
	}
}

// 判重口径必须带上 save_dir / alias：这两个字段决定订阅落到哪个目录，
// 「同一个仓库按不同白名单拆存两个目录」是青龙生态的常见用法，漏掉它们就是把合法配置一起拒掉。
func TestCreateSubscriptionAllowsSameRepoWithDifferentSaveDir(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "dup-sub-savedir-operator", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()

	// 第一条：只取 jd_*.js，落到 jd 目录。
	jdBody := `{"name":"仓库A 京东","type":"git-repo","url":"https://github.com/example/a.git","branch":"main","whitelist":"jd_*.js","save_dir":"jd"}`
	if rec := postJSON(t, engine, "/api/v1/subscriptions", jdBody, token); rec.Code != http.StatusCreated {
		t.Fatalf("首次创建应成功，实际 %d，body=%s", rec.Code, rec.Body.String())
	}

	// 第二条：url / branch / sub_path 与第一条逐字相同，只有 save_dir 不同，必须放行。
	libsBody := `{"name":"仓库A 工具","type":"git-repo","url":"https://github.com/example/a.git","branch":"main","whitelist":"utils/*.js","save_dir":"libs"}`
	if rec := postJSON(t, engine, "/api/v1/subscriptions", libsBody, token); rec.Code != http.StatusCreated {
		t.Fatalf("同仓库不同保存目录应放行，实际 %d，body=%s", rec.Code, rec.Body.String())
	}

	// alias 同理：save_dir 为空时它才是最终落盘目录。
	aliasBody := `{"name":"仓库A 别名","type":"git-repo","url":"https://github.com/example/a.git","branch":"main","alias":"repo-a-mirror"}`
	if rec := postJSON(t, engine, "/api/v1/subscriptions", aliasBody, token); rec.Code != http.StatusCreated {
		t.Fatalf("同仓库不同别名应放行，实际 %d，body=%s", rec.Code, rec.Body.String())
	}

	// 五个字段全同才算重复，这一条必须被拦住 —— 否则连点防重就整个失效了。
	assertDuplicateRejected(t, postJSON(t, engine, "/api/v1/subscriptions", libsBody, token), "相同地址、分支、子目录、保存目录和别名的订阅已存在")

	var count int64
	database.DB.Model(&model.Subscription{}).Where("url = ?", "https://github.com/example/a.git").Count(&count)
	if count != 3 {
		t.Fatalf("期望三条订阅（jd / libs / alias），实际 %d 条", count)
	}
}
