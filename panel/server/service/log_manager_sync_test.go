package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 去掉「每写一个片段就 fsync 一次」之后，落盘内容必须一个字节都不差：
// 顺序不能乱、裸 \r 不能被吃掉、也不能凭空多出换行。
// （fsync 本身没法直接断言，这里守的是「去掉它没有改变可观察行为」。）
func TestLogStreamManagerWriteKeepsExactBytes(t *testing.T) {
	dir := t.TempDir()
	// 故意用一层还不存在的子目录，顺带守住 Write 里的 MkdirAll。
	path := filepath.Join(dir, "task_1_测试", "run.log")

	mgr := &LogStreamManager{
		streams:   make(map[string]*os.File),
		fileSizes: make(map[string]int64),
		maxSize:   10 * 1024 * 1024,
	}
	defer mgr.CloseAll()

	chunks := []string{
		"=== 开始执行 ===\n",
		"进度 10%\r",
		"进度 20%\r",
		"进度 100%\r\n",
		"没有换行结尾的一段",
	}
	for _, chunk := range chunks {
		if err := mgr.Write(path, chunk); err != nil {
			t.Fatalf("write %q: %v", chunk, err)
		}
	}
	mgr.CloseStream(path)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	want := strings.Join(chunks, "")
	if string(got) != want {
		t.Fatalf("落盘内容不一致：\n期望 %q\n实际 %q", want, string(got))
	}
}

// 「超限写一次可见标记后停写」的语义在去掉 fsync 之后必须原样保留：
// 标记只写一次，之后的内容一个字节都不再落盘。
func TestLogStreamManagerWriteStopsAfterSizeLimitMarker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task_2", "run.log")

	mgr := &LogStreamManager{
		streams:   make(map[string]*os.File),
		fileSizes: make(map[string]int64),
		maxSize:   8,
	}
	defer mgr.CloseAll()

	if err := mgr.Write(path, "0123456789"); err != nil {
		t.Fatalf("write first chunk: %v", err)
	}
	// 超限之后再怎么写都不该落盘（这里连续写几次，确认标记不会被重复追加）。
	for i := 0; i < 3; i++ {
		if err := mgr.Write(path, "这些内容不该出现"); err != nil {
			t.Fatalf("write after limit: %v", err)
		}
	}
	mgr.CloseStream(path)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}

	const marker = "\n[日志文件已达到大小限制，停止写入]"
	want := "0123456789" + marker
	if string(got) != want {
		t.Fatalf("超限后的落盘内容不对：\n期望 %q\n实际 %q", want, string(got))
	}
	if strings.Count(string(got), marker) != 1 {
		t.Fatalf("超限标记应当只出现一次，实际 %q", string(got))
	}
}
