package dlticket

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testSecret = "download-ticket-test-secret"

func TestIssueAndVerifyRoundTrip(t *testing.T) {
	resource := "task-log-file:12:task_12_签到/run.log"

	ticket, expiresAt, err := Issue(testSecret, resource, "alice", time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !expiresAt.After(time.Now()) {
		t.Fatalf("expected future expiry, got %v", expiresAt)
	}
	// 票据要能直接放进 URL 查询串，不允许出现需要转义的字符。
	if strings.ContainsAny(ticket, "+/= &?#%") {
		t.Fatalf("ticket must be url-safe, got %q", ticket)
	}

	subject, err := Verify(testSecret, ticket, resource)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if subject != "alice" {
		t.Fatalf("expected subject alice, got %q", subject)
	}
}

func TestVerifyRejectsDifferentResource(t *testing.T) {
	ticket, _, err := Issue(testSecret, "task-log-file:12:task_12/a.log", "alice", time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	// 票据绑定单个资源：换成别的任务、别的文件、甚至只差一个 ../ 都必须验签失败。
	for _, resource := range []string{
		"task-log-file:12:task_12/b.log",
		"task-log-file:13:task_12/a.log",
		"task-log-file:12:../../etc/passwd",
		"task-log-record:12",
		"",
	} {
		if _, err := Verify(testSecret, ticket, resource); err == nil {
			t.Fatalf("expected ticket to be rejected for resource %q", resource)
		}
	}
}

func TestVerifyRejectsTamperedOrForeignTicket(t *testing.T) {
	resource := "task-log-record:9"
	ticket, _, err := Issue(testSecret, resource, "alice", time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	parts := strings.Split(ticket, separator)
	if len(parts) != 4 {
		t.Fatalf("unexpected ticket layout: %q", ticket)
	}

	// 把过期时间往后改，但签名不变 —— 必须被识破，而不是延长有效期。
	extended := strings.Join([]string{
		parts[0],
		parts[1],
		strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10),
		parts[3],
	}, separator)

	cases := map[string]string{
		"empty":            "",
		"garbage":          "not-a-ticket",
		"missing segments": strings.Join(parts[:3], separator),
		"bad version":      strings.Join(append([]string{"v0"}, parts[1:]...), separator),
		"extended expiry":  extended,
		"other secret":     mustIssue(t, "another-secret", resource, "alice"),
	}

	for name, ticket := range cases {
		if _, err := Verify(testSecret, ticket, resource); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: expected ErrInvalid, got %v", name, err)
		}
	}
}

func TestVerifyRejectsExpiredTicket(t *testing.T) {
	resource := "task-log-record:9"

	// 直接构造一张签名合法但已经过期的票据，避免依赖 sleep。
	exp := strconv.FormatInt(time.Now().Add(-time.Minute).Unix(), 10)
	encodedSubject := base64.RawURLEncoding.EncodeToString([]byte("alice"))
	expired := strings.Join([]string{
		version,
		encodedSubject,
		exp,
		sign(testSecret, resource, encodedSubject, exp),
	}, separator)

	if _, err := Verify(testSecret, expired, resource); !errors.Is(err, ErrExpired) {
		t.Fatalf("expected ErrExpired, got %v", err)
	}
}

func TestIssueRejectsEmptyInputs(t *testing.T) {
	if _, _, err := Issue("", "task-log-record:1", "alice", time.Minute); err == nil {
		t.Fatal("expected error for empty secret")
	}
	if _, _, err := Issue(testSecret, "  ", "alice", time.Minute); err == nil {
		t.Fatal("expected error for empty resource")
	}
}

func mustIssue(t *testing.T, secret, resource, subject string) string {
	t.Helper()

	ticket, _, err := Issue(secret, resource, subject, time.Minute)
	if err != nil {
		t.Fatalf("issue with secret %q: %v", secret, err)
	}
	return ticket
}
