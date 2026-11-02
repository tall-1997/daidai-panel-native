package trigticket_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"daidai-panel/pkg/trigticket"
)

const (
	testSecret   = "test-secret"
	testResource = "task-trigger:12"
	testSubject  = "admin"
)

func TestIssueAndVerifyNeverExpires(t *testing.T) {
	ticket, expiresAt, err := trigticket.Issue(testSecret, testResource, testSubject, 0, 0)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if !expiresAt.IsZero() {
		t.Fatalf("ttl<=0 时应当返回零值过期时间，实际: %v", expiresAt)
	}

	subject, err := trigticket.Verify(testSecret, ticket, testResource, 0)
	if err != nil {
		t.Fatalf("校验失败: %v", err)
	}
	if subject != testSubject {
		t.Fatalf("subject 不一致，期望 %q 实际 %q", testSubject, subject)
	}
}

// TestVerifyRejectsOtherResource 锁定「一张票挪不到别的任务上」这条核心性质。
func TestVerifyRejectsOtherResource(t *testing.T) {
	ticket, _, err := trigticket.Issue(testSecret, testResource, testSubject, 0, 0)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}

	if _, err := trigticket.Verify(testSecret, ticket, "task-trigger:13", 0); !errors.Is(err, trigticket.ErrInvalid) {
		t.Fatalf("换一个任务应当验签失败，实际: %v", err)
	}
}

// TestVerifyRejectsStaleGeneration 是「作废全部链接」这个功能的唯一保障：
// 代次 +1 之后，之前发出去的票必须立刻失效。
func TestVerifyRejectsStaleGeneration(t *testing.T) {
	ticket, _, err := trigticket.Issue(testSecret, testResource, testSubject, 3, 0)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}

	if _, err := trigticket.Verify(testSecret, ticket, testResource, 3); err != nil {
		t.Fatalf("同代次应当校验通过，实际: %v", err)
	}
	if _, err := trigticket.Verify(testSecret, ticket, testResource, 4); !errors.Is(err, trigticket.ErrInvalid) {
		t.Fatalf("代次 +1 后旧票应当失效，实际: %v", err)
	}
}

func TestVerifyRejectsTamperedTicket(t *testing.T) {
	ticket, _, err := trigticket.Issue(testSecret, testResource, testSubject, 0, 0)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}

	cases := []struct {
		name   string
		secret string
		ticket string
	}{
		{"签名被改", testSecret, ticket[:len(ticket)-1] + "A"},
		{"少一段", testSecret, strings.Join(strings.Split(ticket, ".")[:4], ".")},
		{"版本号不对", testSecret, "v2" + ticket[2:]},
		{"换了另一把钥匙", "another-secret", ticket},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := trigticket.Verify(tc.secret, tc.ticket, testResource, 0); err == nil {
				t.Fatal("应当验签失败")
			}
		})
	}
}

// TestVerifyHonoursExplicitTTL 保证「可选过期」这条支路没写坏：
// 显式给了 ttl 的票据仍然会按时间过期（方案 B 之外的调用方可能需要短期票）。
func TestVerifyHonoursExplicitTTL(t *testing.T) {
	ticket, expiresAt, err := trigticket.Issue(testSecret, testResource, testSubject, 0, -1)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if !expiresAt.IsZero() {
		t.Fatal("ttl 为负数应当按不过期处理")
	}

	// 正数 ttl：签发时间点往前推不了，改用一个极短的 ttl 再等它过去。
	ticket, expiresAt, err = trigticket.Issue(testSecret, testResource, testSubject, 0, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if expiresAt.IsZero() {
		t.Fatal("ttl>0 时应当返回真实过期时间")
	}
	time.Sleep(1100 * time.Millisecond) // 过期时间只精确到秒，要跨过一整秒才算过期
	if _, err := trigticket.Verify(testSecret, ticket, testResource, 0); !errors.Is(err, trigticket.ErrExpired) {
		t.Fatalf("过期票据应当返回 ErrExpired，实际: %v", err)
	}
}

func TestIssueRejectsEmptyInput(t *testing.T) {
	if _, _, err := trigticket.Issue("", testResource, testSubject, 0, 0); err == nil {
		t.Fatal("密钥为空应当报错")
	}
	if _, _, err := trigticket.Issue(testSecret, "  ", testSubject, 0, 0); err == nil {
		t.Fatal("资源标识为空应当报错")
	}
}
