package service

import (
	"testing"
)

// TinyLog.Write 里 remainder 与 data 共用底层数组会写出错乱内容 —— 这个用例守住修复。
//
// 【怎么触发的】
// remainder 只在「上一批数据以不完整 UTF-8 字节收尾」时才非空（多字节汉字被 SSE/管道
// 切片切成两半是最常见的来源）。老写法是：
//
//	data := append(l.remainder, p...)   // remainder 还有富余容量时会【原地】追加
//	l.remainder = l.remainder[:0]       // 于是这两者指向同一块底层数组
//	l.remainder = append(l.remainder, data[i:]...)  // 把尾巴拷回数组开头 = 覆写 data 的头部
//
// 结果 l.writer.Write(data) 写出去的前几个字节变成了本该留到下一批的尾巴。
// 下面这组数据里，"中y" 会被写成 "旭y"（E4 B8 被 E6 97 覆盖）。
//
// 用例直接给 remainder 预留一大块容量，把「是否原地追加」这件事从看运气变成必然，
// 所以它是确定性的，不是碰运气的竞态测试。
func TestTinyLogWriteDoesNotCorruptWhenRemainderIsReused(t *testing.T) {
	tl, err := NewTinyLog("alias-regression")
	if err != nil {
		t.Fatalf("create tiny log: %v", err)
	}
	defer func() {
		_, _ = tl.Close()
	}()

	// "中" = E4 B8 AD，这里先留下前两个字节当作上一批切剩的尾巴。
	// 容量给足，保证下一次 Write 的 append 会走原地追加那条路径。
	tl.remainder = make([]byte, 0, 64)
	tl.remainder = append(tl.remainder, 0xE4, 0xB8)

	// 这一批 = "中" 的最后一个字节 + 'y' + "日"(E6 97 A5) 的前两个字节。
	// 期望：写出 "中y"，把 E6 97 留到下一批。
	if _, err := tl.Write([]byte{0xAD, 'y', 0xE6, 0x97}); err != nil {
		t.Fatalf("write first chunk: %v", err)
	}
	// 补齐 "日" 并换行。
	if _, err := tl.Write([]byte{0xA5, '\n'}); err != nil {
		t.Fatalf("write second chunk: %v", err)
	}

	content, err := tl.ReadAll()
	if err != nil {
		t.Fatalf("read all: %v", err)
	}

	const want = "中y日\n"
	if string(content) != want {
		t.Fatalf("日志内容被切片别名覆写：期望 %q，实际 %q", want, string(content))
	}
}

// 顺带守住「切成两半的多字节字符最终不会丢」这条既有语义：
// 不完整的尾巴要留到下一批补齐，Close 时若仍不完整也要原样落盘。
func TestTinyLogWriteKeepsSplitRuneAcrossChunks(t *testing.T) {
	tl, err := NewTinyLog("split-rune")
	if err != nil {
		t.Fatalf("create tiny log: %v", err)
	}
	defer func() {
		_, _ = tl.Close()
	}()

	raw := []byte("进度 100% 完成\n")
	for i := 0; i < len(raw); i++ {
		if _, err := tl.Write(raw[i : i+1]); err != nil {
			t.Fatalf("write byte %d: %v", i, err)
		}
	}

	content, err := tl.ReadAll()
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if string(content) != string(raw) {
		t.Fatalf("逐字节写入后内容不一致：期望 %q，实际 %q", string(raw), string(content))
	}
}
