package handler_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/pkg/dlticket"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

// 「下载原始日志」的核心承诺：拿到的字节和磁盘上的日志文件完全一致。
// 这段内容里的裸 \r 和 ANSI 序列正是前端三处日志展示会折叠 / 转义掉的部分，
// 一旦下载链路里混进了折叠逻辑，下面的逐字节断言就会失败。
const rawLogFixture = "拉取中 10%\r拉取中 60%\r拉取中 100%\r\n\x1b[32m✔ 完成\x1b[0m\n"

func TestRawLogRecordDownloadStreamsExactDiskBytes(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "raw-log-viewer", "viewer")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := mustCreateRawLogTask(t, "京东签到/任务")
	relPath := fmt.Sprintf("task_%d_京东签到_任务/2026-08-04-10-00-00-000.log", task.ID)
	writeRawLogFile(t, relPath, rawLogFixture)
	taskLog := mustCreateRawTaskLog(t, task.ID, &relPath)

	payload := requestRawTicket(t, engine, token, fmt.Sprintf("/api/v1/logs/%d/raw-ticket", taskLog.ID))

	if got, want := payload["filename"], fmt.Sprintf("京东签到_任务-%d-raw.log", taskLog.ID); got != want {
		t.Fatalf("expected download filename %q, got %#v", want, got)
	}
	if got, want := payload["size"], float64(len(rawLogFixture)); got != want {
		t.Fatalf("expected size %v, got %#v", want, got)
	}

	downloadURL, _ := payload["url"].(string)
	if !strings.HasPrefix(downloadURL, fmt.Sprintf("/api/v1/logs/%d/raw?", taskLog.ID)) {
		t.Fatalf("unexpected download url: %q", downloadURL)
	}

	// 走浏览器原生下载：只有 URL 上的票据，没有 Authorization 头。
	rec := performRequest(engine, http.MethodGet, downloadURL, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), []byte(rawLogFixture)) {
		t.Fatalf("raw download must be byte-identical to the file on disk, got %q", rec.Body.String())
	}
	if got, want := rec.Header().Get("Content-Length"), fmt.Sprintf("%d", len(rawLogFixture)); got != want {
		t.Fatalf("expected Content-Length %s, got %q", want, got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate" {
		t.Fatalf("expected download response to disable cache, got %q", got)
	}

	assertAttachmentDisposition(t, rec.Header().Get("Content-Disposition"))
}

func TestRawLogFileDownloadStreamsExactDiskBytes(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "raw-file-viewer", "viewer")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := mustCreateRawLogTask(t, "签到任务")
	filename := "2026-08-04-10-00-00-000.log"
	relPath := fmt.Sprintf("task_%d_签到任务/%s", task.ID, filename)
	writeRawLogFile(t, relPath, rawLogFixture)

	ticketURL := fmt.Sprintf("/api/v1/tasks/%d/log-files/%s/raw-ticket?path=%s",
		task.ID, url.PathEscape(filename), url.QueryEscape(relPath))
	payload := requestRawTicket(t, engine, token, ticketURL)

	if got := payload["filename"]; got != filename {
		t.Fatalf("expected download filename %q, got %#v", filename, got)
	}

	downloadURL, _ := payload["url"].(string)
	if !strings.Contains(downloadURL, fmt.Sprintf("/api/v1/tasks/%d/log-files/%s/raw?", task.ID, filename)) {
		t.Fatalf("unexpected download url: %q", downloadURL)
	}
	// 下载 URL 必须原样带回定位参数，否则算出的资源标识对不上，验签会失败。
	if !strings.Contains(downloadURL, "path=") {
		t.Fatalf("expected download url to carry the path locator, got %q", downloadURL)
	}

	rec := performRequest(engine, http.MethodGet, downloadURL, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), []byte(rawLogFixture)) {
		t.Fatalf("raw download must be byte-identical to the file on disk, got %q", rec.Body.String())
	}

	assertAttachmentDisposition(t, rec.Header().Get("Content-Disposition"))
}

func TestRawLogDownloadRejectsPathTraversalAttempts(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "raw-traversal-admin", "admin")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	// 日志根目录之外的敏感文件，任何一次穿越尝试都不允许把它读出来。
	secretPath := filepath.Join(config.C.Data.Dir, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("top-secret-payload"), 0o644); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	task := mustCreateRawLogTask(t, "签到任务")
	writeRawLogFile(t, fmt.Sprintf("task_%d_签到任务/run.log", task.ID), rawLogFixture)

	// encodedPath 是直接拼进 URL 查询串的值，所以这里存的是「已编码」形式：
	// 需要考察服务端解码后的行为时（%2e%2e%2f -> ../），就不能再套一层 QueryEscape。
	attempts := []struct {
		name        string
		encodedPath string
	}{
		{"parent traversal", url.QueryEscape("../secret.txt")},
		{"nested parent traversal", url.QueryEscape(fmt.Sprintf("task_%d_签到任务/../../secret.txt", task.ID))},
		{"deep traversal", url.QueryEscape("../../../../etc/passwd")},
		{"absolute posix path", url.QueryEscape("/etc/passwd")},
		{"absolute host path", url.QueryEscape(secretPath)},
		{"windows style traversal", url.QueryEscape(`..\secret.txt`)},
		{"dot slash prefix", url.QueryEscape("./../secret.txt")},
		{"another task dir", url.QueryEscape("task_999_别人的任务/run.log")},
		// 单层编码：gin 解码后就是 ../secret.txt
		{"url encoded traversal", "%2e%2e%2fsecret.txt"},
		{"url encoded slash", "..%2Fsecret.txt"},
		// 双层编码：解码后是字面量 %2e%2e%2fsecret.txt，不能被再解一次
		{"double encoded traversal", "%252e%252e%252fsecret.txt"},
	}

	for _, attempt := range attempts {
		ticketURL := fmt.Sprintf("/api/v1/tasks/%d/log-files/run.log/raw-ticket?path=%s",
			task.ID, attempt.encodedPath)
		rec := performRequest(engine, http.MethodGet, ticketURL, map[string]string{"Authorization": "Bearer " + token})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: expected 404 from ticket endpoint, got %d, body=%s", attempt.name, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "top-secret-payload") {
			t.Fatalf("%s: ticket endpoint leaked file content", attempt.name)
		}
	}

	// 路径参数里的穿越（不走 ?path=）连路由都匹配不上。
	for _, rawPath := range []string{
		fmt.Sprintf("/api/v1/tasks/%d/log-files/..%%2F..%%2Fsecret.txt/raw-ticket", task.ID),
		fmt.Sprintf("/api/v1/tasks/%d/log-files/..%%2F..%%2Fsecret.txt/raw", task.ID),
	} {
		rec := performRequest(engine, http.MethodGet, rawPath, map[string]string{"Authorization": "Bearer " + token})
		if rec.Code == http.StatusOK {
			t.Fatalf("expected traversal in the url path to be rejected, got 200 for %s", rawPath)
		}
		if strings.Contains(rec.Body.String(), "top-secret-payload") {
			t.Fatalf("traversal in the url path leaked file content: %s", rawPath)
		}
	}

	// 纵深防御：就算攻击者拿到一张为穿越路径签发的合法票据（模拟签发环节被绕过），
	// 下载接口自己的路径解析仍必须把它拦下来。
	for _, locator := range []string{"../secret.txt", secretPath} {
		resource := fmt.Sprintf("task-log-file:%d:%s", task.ID, locator)
		ticket, _, err := dlticket.Issue(config.C.JWT.Secret, resource, "attacker", time.Minute)
		if err != nil {
			t.Fatalf("issue forged ticket: %v", err)
		}

		downloadURL := fmt.Sprintf("/api/v1/tasks/%d/log-files/run.log/raw?path=%s&ticket=%s",
			task.ID, url.QueryEscape(locator), url.QueryEscape(ticket))
		rec := performRequest(engine, http.MethodGet, downloadURL, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("forged ticket for %q: expected 404, got %d, body=%s", locator, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "top-secret-payload") {
			t.Fatalf("forged ticket for %q leaked file content", locator)
		}
	}
}

func TestRawLogDownloadRequiresAuthorization(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "raw-auth-viewer", "viewer")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := mustCreateRawLogTask(t, "签到任务")
	relPath := fmt.Sprintf("task_%d_签到任务/run.log", task.ID)
	writeRawLogFile(t, relPath, rawLogFixture)
	taskLog := mustCreateRawTaskLog(t, task.ID, &relPath)

	otherRelPath := fmt.Sprintf("task_%d_签到任务/other.log", task.ID)
	writeRawLogFile(t, otherRelPath, "other")
	otherLog := mustCreateRawTaskLog(t, task.ID, &otherRelPath)

	// 1. 签发接口和其它日志接口一样必须登录。
	for _, path := range []string{
		fmt.Sprintf("/api/v1/logs/%d/raw-ticket", taskLog.ID),
		fmt.Sprintf("/api/v1/tasks/%d/log-files/run.log/raw-ticket", task.ID),
	} {
		rec := performRequest(engine, http.MethodGet, path, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 without token for %s, got %d, body=%s", path, rec.Code, rec.Body.String())
		}
	}

	// 2. 下载接口没有票据 / 票据无效时一律 401，且不能吐出任何文件内容。
	for name, path := range map[string]string{
		"no ticket":       fmt.Sprintf("/api/v1/logs/%d/raw", taskLog.ID),
		"empty ticket":    fmt.Sprintf("/api/v1/logs/%d/raw?ticket=", taskLog.ID),
		"garbage ticket":  fmt.Sprintf("/api/v1/logs/%d/raw?ticket=not-a-ticket", taskLog.ID),
		"bearer as query": fmt.Sprintf("/api/v1/logs/%d/raw?ticket=%s", taskLog.ID, url.QueryEscape(token)),
	} {
		rec := performRequest(engine, http.MethodGet, path, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: expected 401, got %d, body=%s", name, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "拉取中") {
			t.Fatalf("%s: unauthorized request must not return log content", name)
		}
	}

	// 3. Authorization 头本身不能替代票据 —— 否则「浏览器原生下载」这条路会被误认为已鉴权。
	rec := performRequest(engine, http.MethodGet,
		fmt.Sprintf("/api/v1/logs/%d/raw", taskLog.ID),
		map[string]string{"Authorization": "Bearer " + token})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without ticket even with a bearer token, got %d", rec.Code)
	}

	// 4. 票据绑定单个资源：拿 A 日志的票去下 B 日志必须失败。
	payload := requestRawTicket(t, engine, token, fmt.Sprintf("/api/v1/logs/%d/raw-ticket", otherLog.ID))
	otherURL, _ := payload["url"].(string)
	swapped := strings.Replace(otherURL,
		fmt.Sprintf("/logs/%d/raw", otherLog.ID),
		fmt.Sprintf("/logs/%d/raw", taskLog.ID), 1)
	rec = performRequest(engine, http.MethodGet, swapped, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when reusing another log's ticket, got %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestRawLogDownloadReturnsNotFoundInsteadOfServerError(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "raw-missing-viewer", "viewer")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := mustCreateRawLogTask(t, "签到任务")

	// 日志记录存在，但磁盘文件已经被日志清理删掉。
	missingRelPath := fmt.Sprintf("task_%d_签到任务/cleaned.log", task.ID)
	cleanedLog := mustCreateRawTaskLog(t, task.ID, &missingRelPath)

	// 内容只存在数据库里的短日志，压根没有独立的原始文件。
	inlineLog := mustCreateRawTaskLog(t, task.ID, nil)

	for name, path := range map[string]string{
		"cleaned file":      fmt.Sprintf("/api/v1/logs/%d/raw-ticket", cleanedLog.ID),
		"db-only content":   fmt.Sprintf("/api/v1/logs/%d/raw-ticket", inlineLog.ID),
		"unknown log id":    "/api/v1/logs/999999/raw-ticket",
		"unknown log file":  fmt.Sprintf("/api/v1/tasks/%d/log-files/nope.log/raw-ticket", task.ID),
		"unknown task logs": "/api/v1/tasks/424242/log-files/nope.log/raw-ticket",
	} {
		rec := performRequest(engine, http.MethodGet, path, map[string]string{"Authorization": "Bearer " + token})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: expected 404, got %d, body=%s", name, rec.Code, rec.Body.String())
		}
	}

	// 票据签发成功之后文件才被清理：下载接口同样要回 404，不能 500。
	presentRelPath := fmt.Sprintf("task_%d_签到任务/present.log", task.ID)
	fullPath := writeRawLogFile(t, presentRelPath, rawLogFixture)
	presentLog := mustCreateRawTaskLog(t, task.ID, &presentRelPath)

	payload := requestRawTicket(t, engine, token, fmt.Sprintf("/api/v1/logs/%d/raw-ticket", presentLog.ID))
	downloadURL, _ := payload["url"].(string)

	if err := os.Remove(fullPath); err != nil {
		t.Fatalf("remove log file: %v", err)
	}

	rec := performRequest(engine, http.MethodGet, downloadURL, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after the file was cleaned, got %d, body=%s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func assertAttachmentDisposition(t *testing.T, disposition string) {
	t.Helper()

	if !strings.HasPrefix(disposition, "attachment; ") {
		t.Fatalf("expected an attachment disposition, got %q", disposition)
	}
	if !strings.Contains(disposition, `filename="`) {
		t.Fatalf("expected an ascii filename fallback, got %q", disposition)
	}
	if !strings.Contains(disposition, "filename*=UTF-8''") {
		t.Fatalf("expected an RFC 5987 filename* parameter, got %q", disposition)
	}
	// 响应头里出现裸 UTF-8 字节会被浏览器按 latin-1 解成乱码，中文名必须已经百分号编码。
	for i := 0; i < len(disposition); i++ {
		if disposition[i] < 0x20 || disposition[i] > 0x7e {
			t.Fatalf("Content-Disposition must stay ascii-only, got %q", disposition)
		}
	}
}

func requestRawTicket(t *testing.T, engine *gin.Engine, token, path string) map[string]interface{} {
	t.Helper()

	rec := performRequest(engine, http.MethodGet, path, map[string]string{"Authorization": "Bearer " + token})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from %s, got %d, body=%s", path, rec.Code, rec.Body.String())
	}
	return decodeJSONMap(t, rec)
}

func mustCreateRawLogTask(t *testing.T, name string) *model.Task {
	t.Helper()

	task := &model.Task{
		Name:           name,
		Command:        "echo raw",
		CronExpression: "0 0 * * *",
		Status:         model.TaskStatusEnabled,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}

func mustCreateRawTaskLog(t *testing.T, taskID uint, relPath *string) *model.TaskLog {
	t.Helper()

	status := model.LogStatusSuccess
	taskLog := &model.TaskLog{
		TaskID:    taskID,
		Status:    &status,
		LogPath:   relPath,
		StartedAt: time.Now(),
	}
	if err := database.DB.Create(taskLog).Error; err != nil {
		t.Fatalf("create task log: %v", err)
	}
	return taskLog
}

func writeRawLogFile(t *testing.T, relPath, content string) string {
	t.Helper()

	fullPath := filepath.Join(config.C.Data.LogDir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("create log dir: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write log file: %v", err)
	}
	return fullPath
}
