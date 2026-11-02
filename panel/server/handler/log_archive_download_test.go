package handler_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/testutil"
)

// 「日志文件夹打包下载」的核心承诺和单文件下载完全一致：
// zip 里每个 entry 的字节必须和磁盘上那份一模一样。
// 复用 rawLogFixture（含裸 \r 与 ANSI 序列），一旦打包链路里混进了前端那套折叠逻辑，
// 下面的逐字节断言就会失败。
func TestLogArchiveDownloadPacksExactDiskBytes(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "archive-viewer", "viewer")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := mustCreateRawLogTask(t, "京东签到/任务")

	// 任务改过名会留下多个 task_<id>[_名字] 目录并存，两边还可能有同名文件。
	// zip 内路径直接用日志根目录内的相对路径，所以层级天然保留、不会撞名。
	legacyDirPath := fmt.Sprintf("task_%d/2026-08-04-10-00-00-000.log", task.ID)
	renamedDirPath := fmt.Sprintf("task_%d_京东签到_任务/2026-08-04-10-00-00-000.log", task.ID)
	renamedDirNextDay := fmt.Sprintf("task_%d_京东签到_任务/2026-08-05-10-00-00-000.log", task.ID)
	wantFiles := map[string]string{
		legacyDirPath:     rawLogFixture,
		renamedDirPath:    "改名之后的那一份\r\n",
		renamedDirNextDay: "第二天 \x1b[31m出错\x1b[0m\n",
	}
	totalBytes := 0
	for relPath, content := range wantFiles {
		writeRawLogFile(t, relPath, content)
		totalBytes += len(content)
	}

	payload := requestRawTicket(t, engine, token,
		fmt.Sprintf("/api/v1/tasks/%d/log-files/archive-ticket", task.ID))

	if got, want := payload["file_count"], float64(len(wantFiles)); got != want {
		t.Fatalf("expected file_count %v, got %#v", want, got)
	}
	// size 的语义是未压缩总字节，前端拿它做体积提示与上限预判。
	if got, want := payload["size"], float64(totalBytes); got != want {
		t.Fatalf("expected size %v, got %#v", want, got)
	}
	filename, _ := payload["filename"].(string)
	// 任务名里的 / 会被 sanitizeDownloadFilename 换成 _，中文原样保留。
	if !strings.HasPrefix(filename, "京东签到_任务-logs-") || !strings.HasSuffix(filename, ".zip") {
		t.Fatalf("unexpected archive filename: %q", filename)
	}

	downloadURL, _ := payload["url"].(string)
	if !strings.HasPrefix(downloadURL, fmt.Sprintf("/api/v1/tasks/%d/log-files/archive?", task.ID)) {
		t.Fatalf("unexpected download url: %q", downloadURL)
	}

	// 走浏览器原生下载：只有 URL 上的票据，没有 Authorization 头。
	rec := performRequest(engine, http.MethodGet, downloadURL, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("expected application/zip, got %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected the archive response to disable cache, got %q", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected nosniff, got %q", got)
	}
	// 流式 zip 事先算不出长度、也没法定位任意偏移，声称支持 Range 会让续传拿到截断的包。
	if got := rec.Header().Get("Accept-Ranges"); got != "" {
		t.Fatalf("streaming zip must not advertise Range support, got %q", got)
	}
	assertAttachmentDisposition(t, rec.Header().Get("Content-Disposition"))

	body := rec.Body.Bytes()
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("read back the archive: %v", err)
	}
	if len(reader.File) != len(wantFiles) {
		t.Fatalf("expected %d entries in the archive, got %d", len(wantFiles), len(reader.File))
	}
	for _, entry := range reader.File {
		expected, ok := wantFiles[entry.Name]
		if !ok {
			t.Fatalf("unexpected zip entry %q", entry.Name)
		}
		rc, err := entry.Open()
		if err != nil {
			t.Fatalf("open zip entry %q: %v", entry.Name, err)
		}
		got, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read zip entry %q: %v", entry.Name, err)
		}
		if !bytes.Equal(got, []byte(expected)) {
			t.Fatalf("zip entry %q must be byte-identical to the file on disk, got %q", entry.Name, string(got))
		}
	}
}

// 范围筛选按磁盘 ModTime（也就是列表接口 created_at 那一列），
// 前端的「最近 7 天 / 最近 30 天」全靠它把超上限的任务缩回可下载的量。
func TestLogArchiveDownloadHonorsTimeRange(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "archive-range-viewer", "viewer")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := mustCreateRawLogTask(t, "签到任务")
	oldPath := fmt.Sprintf("task_%d/2026-07-01-10-00-00-000.log", task.ID)
	newPath := fmt.Sprintf("task_%d/2026-08-04-10-00-00-000.log", task.ID)
	oldFull := writeRawLogFile(t, oldPath, "很久以前的日志\n")
	writeRawLogFile(t, newPath, rawLogFixture)

	staleTime := time.Now().AddDate(0, 0, -30)
	if err := os.Chtimes(oldFull, staleTime, staleTime); err != nil {
		t.Fatalf("backdate the old log file: %v", err)
	}

	start := time.Now().AddDate(0, 0, -7)
	payload := requestRawTicket(t, engine, token,
		fmt.Sprintf("/api/v1/tasks/%d/log-files/archive-ticket?start=%s",
			task.ID, url.QueryEscape(start.Format(time.RFC3339))))

	if got, want := payload["file_count"], float64(1); got != want {
		t.Fatalf("expected file_count %v after filtering, got %#v", want, got)
	}

	downloadURL, _ := payload["url"].(string)
	// 下载 URL 必须原样带回 start，否则资源标识对不上、验签会失败。
	if !strings.Contains(downloadURL, "start=") {
		t.Fatalf("expected download url to carry the range locator, got %q", downloadURL)
	}

	rec := performRequest(engine, http.MethodGet, downloadURL, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	body := rec.Body.Bytes()
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("read back the archive: %v", err)
	}
	if len(reader.File) != 1 || reader.File[0].Name != newPath {
		names := make([]string, 0, len(reader.File))
		for _, entry := range reader.File {
			names = append(names, entry.Name)
		}
		t.Fatalf("expected only %q in the archive, got %v", newPath, names)
	}
}

// 票据签发之后 120 秒内都可以拿去下载，这期间日志保留清理（service/log_manager.go）
// 完全可能删掉其中几个文件 —— 收集列表到真正 os.Open 之间也有同样的窗口。
//
// 这时候唯一可接受的结果是：**少了那一个、其余照常**，包本身仍然完好。
// 反面教材是「遇到失败就 break、然后照常 zw.Close()」：那样写出来的 zip 结构完好、
// 能双击打开、浏览器显示「下载完成」，却静默少了这个文件【之后的全部文件】，用户毫无察觉。
//
// 说明一下这条用例卡的是哪一层：删掉的文件会先被 collectTaskLogArchiveEntries 的
// stat 过滤掉（下载时会完整重跑一遍收集），所以走到写入循环时它已经不在列表里了；
// DownloadLogArchive 里对 os.Open not-exist 的 continue 分支，兜的是「收集之后、写入之前」
// 这段更窄的窗口。两层的口径必须一致，这条用例钉住的就是对外可见的那个承诺。
func TestLogArchiveDownloadSkipsFilesDeletedAfterTicket(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "archive-race-viewer", "viewer")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := mustCreateRawLogTask(t, "会被清理的任务")
	firstPath := fmt.Sprintf("task_%d/2026-08-04-10-00-00-000.log", task.ID)
	doomedPath := fmt.Sprintf("task_%d/2026-08-05-10-00-00-000.log", task.ID)
	lastPath := fmt.Sprintf("task_%d/2026-08-06-10-00-00-000.log", task.ID)
	contents := map[string]string{
		firstPath:  rawLogFixture,
		doomedPath: "这一份马上会被日志清理删掉\n",
		lastPath:   "排在被删文件【后面】的这一份，必须还在包里\r\n\x1b[32mok\x1b[0m\n",
	}
	fullPaths := map[string]string{}
	for relPath, content := range contents {
		fullPaths[relPath] = writeRawLogFile(t, relPath, content)
	}

	// 1. 签票时三个文件都还在。
	payload := requestRawTicket(t, engine, token,
		fmt.Sprintf("/api/v1/tasks/%d/log-files/archive-ticket", task.ID))
	if got, want := payload["file_count"], float64(len(contents)); got != want {
		t.Fatalf("expected file_count %v at ticket time, got %#v", want, got)
	}
	downloadURL, _ := payload["url"].(string)

	// 2. 签票之后、下载之前，日志清理删掉中间那一个。
	if err := os.Remove(fullPaths[doomedPath]); err != nil {
		t.Fatalf("simulate the retention cleanup: %v", err)
	}

	// 3. 下载仍然要成功，而不是 404 / 500 / 半截包。
	rec := performRequest(engine, http.MethodGet, downloadURL, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after a benign cleanup race, got %d, body=%s", rec.Code, rec.Body.String())
	}

	// 4. 包必须是完整的 zip：中央目录写全了才读得回来。
	body := rec.Body.Bytes()
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("the archive must stay a complete, readable zip after skipping a deleted file: %v", err)
	}

	// 5. 只少了被删的那一个，排在它后面的文件不能被连坐。
	got := map[string]string{}
	for _, entry := range reader.File {
		rc, err := entry.Open()
		if err != nil {
			t.Fatalf("open zip entry %q: %v", entry.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read zip entry %q: %v", entry.Name, err)
		}
		got[entry.Name] = string(data)
	}
	if _, ok := got[doomedPath]; ok {
		t.Fatalf("the deleted file must not appear in the archive, got entries %v", keysOf(got))
	}
	for _, relPath := range []string{firstPath, lastPath} {
		data, ok := got[relPath]
		if !ok {
			t.Fatalf("expected %q to survive the cleanup race, got entries %v", relPath, keysOf(got))
		}
		// 幸存的文件必须逐字节完好 —— 跳过一个文件不能让后面的内容错位。
		if data != contents[relPath] {
			t.Fatalf("zip entry %q must be byte-identical to the file on disk, got %q", relPath, data)
		}
	}
	if len(got) != len(contents)-1 {
		t.Fatalf("expected exactly %d entries after the cleanup, got %v", len(contents)-1, keysOf(got))
	}
}

func keysOf(m map[string]string) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	return names
}

func TestLogArchiveDownloadRequiresTicket(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "archive-auth-viewer", "viewer")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := mustCreateRawLogTask(t, "签到任务")
	other := mustCreateRawLogTask(t, "另一个任务")
	writeRawLogFile(t, fmt.Sprintf("task_%d/run.log", task.ID), rawLogFixture)
	writeRawLogFile(t, fmt.Sprintf("task_%d/run.log", other.ID), "别人的日志\n")

	// 1. 签票接口和其它日志接口一样必须登录。
	rec := performRequest(engine, http.MethodGet,
		fmt.Sprintf("/api/v1/tasks/%d/log-files/archive-ticket", task.ID), nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d, body=%s", rec.Code, rec.Body.String())
	}

	// 2. 下载接口没有票据 / 票据无效时一律 401，且不能吐出任何文件内容。
	for name, path := range map[string]string{
		"no ticket":       fmt.Sprintf("/api/v1/tasks/%d/log-files/archive", task.ID),
		"empty ticket":    fmt.Sprintf("/api/v1/tasks/%d/log-files/archive?ticket=", task.ID),
		"garbage ticket":  fmt.Sprintf("/api/v1/tasks/%d/log-files/archive?ticket=not-a-ticket", task.ID),
		"bearer as query": fmt.Sprintf("/api/v1/tasks/%d/log-files/archive?ticket=%s", task.ID, url.QueryEscape(token)),
	} {
		rec := performRequest(engine, http.MethodGet, path, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: expected 401, got %d, body=%s", name, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "拉取中") {
			t.Fatalf("%s: unauthorized request must not return log content", name)
		}
	}

	// 3. Authorization 头本身不能替代票据。
	rec = performRequest(engine, http.MethodGet,
		fmt.Sprintf("/api/v1/tasks/%d/log-files/archive", task.ID),
		map[string]string{"Authorization": "Bearer " + token})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without ticket even with a bearer token, got %d", rec.Code)
	}

	// 4. 票据绑定单个任务：拿 A 任务的票去下 B 任务必须失败。
	payload := requestRawTicket(t, engine, token,
		fmt.Sprintf("/api/v1/tasks/%d/log-files/archive-ticket", other.ID))
	otherURL, _ := payload["url"].(string)
	swapped := strings.Replace(otherURL,
		fmt.Sprintf("/tasks/%d/", other.ID),
		fmt.Sprintf("/tasks/%d/", task.ID), 1)
	rec = performRequest(engine, http.MethodGet, swapped, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when reusing another task's ticket, got %d, body=%s", rec.Code, rec.Body.String())
	}

	// 5. 票据也绑定了原样的 start/end：偷偷放大范围换不到同一张票。
	payload = requestRawTicket(t, engine, token,
		fmt.Sprintf("/api/v1/tasks/%d/log-files/archive-ticket", task.ID))
	scopedURL, _ := payload["url"].(string)
	for name, tampered := range map[string]string{
		"appended end":   scopedURL + "&end=" + url.QueryEscape("2099-01-01T00:00:00Z"),
		"appended start": scopedURL + "&start=" + url.QueryEscape("2000-01-01T00:00:00Z"),
	} {
		rec := performRequest(engine, http.MethodGet, tampered, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: expected 401 after tampering with the range, got %d, body=%s", name, rec.Code, rec.Body.String())
		}
	}
}

func TestLogArchiveDownloadTicketRejectsOversizedArchive(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "archive-limit-viewer", "viewer")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := mustCreateRawLogTask(t, "高频任务")

	// 上限是 2000 个文件（handler/log_archive_download.go 的 maxArchiveFiles）——
	// 5 分钟频率的任务留 7 天就有 2016 个文件，正是这条上限要挡住的真实场景。
	// 这里只造 2001 个 1 字节的小文件压文件数，不去真的写满 512MB。
	dir := filepath.Join(config.C.Data.LogDir, fmt.Sprintf("task_%d", task.ID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create log dir: %v", err)
	}
	for i := 0; i <= 2000; i++ {
		name := filepath.Join(dir, fmt.Sprintf("run-%04d.log", i))
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatalf("write log file: %v", err)
		}
	}

	rec := performRequest(engine, http.MethodGet,
		fmt.Sprintf("/api/v1/tasks/%d/log-files/archive-ticket", task.ID),
		map[string]string{"Authorization": "Bearer " + token})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 over the archive limit, got %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "超过打包上限") {
		t.Fatalf("expected the limit to be spelled out, got %s", body)
	}
	// 文案必须给出路，否则用户看到「超限」也不知道下一步该点什么。
	if !strings.Contains(body, "最近 7 天") {
		t.Fatalf("expected the error to suggest a narrower range, got %s", body)
	}
}

func TestLogArchiveDownloadTicketReturnsNotFoundWithoutLogFiles(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "archive-empty-viewer", "viewer")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := mustCreateRawLogTask(t, "从没跑过的任务")

	rec := performRequest(engine, http.MethodGet,
		fmt.Sprintf("/api/v1/tasks/%d/log-files/archive-ticket", task.ID),
		map[string]string{"Authorization": "Bearer " + token})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 without any log file, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "暂无日志文件") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}
