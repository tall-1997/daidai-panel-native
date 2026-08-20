// Package dlticket 提供「短期 + 绑定单个资源」的下载票据。
//
// 背景：面板所有接口都用 `Authorization: Bearer <jwt>` 鉴权，而浏览器原生下载
// （`<a download>` / `window.open`）无法携带自定义请求头。如果为了让浏览器直接
// 拉流就把下载接口改成匿名可访问，等于开了一个不用登录就能读日志文件的口子。
//
// 这里的做法是「先换票再下载」：
//  1. 前端带 Authorization 头调用签发接口，服务端走完常规鉴权 + 资源定位后才签票；
//  2. 票据用 HMAC-SHA256 签名，签名原文里包含「资源标识」，但资源标识本身不随票据
//     传输 —— 校验方必须自己算出同一个资源标识才可能验签通过，所以票据无法被挪用
//     到别的文件；
//  3. 票据自带过期时间（默认 60s），只够覆盖「拿到票据 → 浏览器发起下载」这一瞬间，
//     泄漏窗口极短。
//
// 票据是无状态的（不落库、不占内存），因此不是「一次性」的：在有效期内可以重复使用。
// 这对下载场景是可接受的，也让浏览器的断点续传 / Range 重试不会失败。
package dlticket

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultTTL 是票据默认有效期。不要放长：票据会出现在 URL 里，
	// 可能被浏览器历史、反向代理访问日志记录下来。
	DefaultTTL = 60 * time.Second

	version   = "v1"
	separator = "."

	// domain 做域分隔。面板复用同一把 JWT secret 做多种签名，
	// 加上固定域前缀可以避免票据签名被挪用到别的 HMAC 场景。
	domain = "daidai-panel/download-ticket/v1"
)

var (
	// ErrInvalid 表示票据格式错误或签名不匹配（含被篡改、资源不对应）。
	ErrInvalid = errors.New("下载票据无效")
	// ErrExpired 表示签名有效但已过期。
	ErrExpired = errors.New("下载票据已过期")
)

// Issue 为 resource 签发一张有效期 ttl 的下载票据，subject 通常是签发时的登录用户名。
// ttl <= 0 时使用 DefaultTTL。
func Issue(secret, resource, subject string, ttl time.Duration) (string, time.Time, error) {
	if strings.TrimSpace(secret) == "" {
		return "", time.Time{}, errors.New("签名密钥为空")
	}
	if strings.TrimSpace(resource) == "" {
		return "", time.Time{}, errors.New("资源标识为空")
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}

	expiresAt := time.Now().Add(ttl)
	exp := strconv.FormatInt(expiresAt.Unix(), 10)
	encodedSubject := base64.RawURLEncoding.EncodeToString([]byte(subject))

	ticket := strings.Join([]string{
		version,
		encodedSubject,
		exp,
		sign(secret, resource, encodedSubject, exp),
	}, separator)

	return ticket, expiresAt, nil
}

// Verify 校验票据是否为 resource 签发且仍在有效期内，成功时返回签发时的 subject。
func Verify(secret, ticket, resource string) (string, error) {
	if strings.TrimSpace(secret) == "" || strings.TrimSpace(resource) == "" {
		return "", ErrInvalid
	}

	parts := strings.Split(strings.TrimSpace(ticket), separator)
	if len(parts) != 4 || parts[0] != version {
		return "", ErrInvalid
	}
	encodedSubject, exp, signature := parts[1], parts[2], parts[3]

	// 先验签再看过期：签名没过关的票据里没有任何字段可信，包括那个时间戳。
	expected := sign(secret, resource, encodedSubject, exp)
	if subtle.ConstantTimeCompare([]byte(signature), []byte(expected)) != 1 {
		return "", ErrInvalid
	}

	expUnix, err := strconv.ParseInt(exp, 10, 64)
	if err != nil {
		return "", ErrInvalid
	}
	if time.Now().After(time.Unix(expUnix, 0)) {
		return "", ErrExpired
	}

	subject, err := base64.RawURLEncoding.DecodeString(encodedSubject)
	if err != nil {
		return "", ErrInvalid
	}
	return string(subject), nil
}

func sign(secret, resource, encodedSubject, exp string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	// 每个字段带 8 字节长度前缀。资源标识里含有日志文件相对路径，
	// 不加长度前缀的话，不同的 (taskID, path) 组合有可能拼出同一段签名原文。
	for _, field := range []string{domain, resource, encodedSubject, exp} {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(field)))
		mac.Write(length[:])
		mac.Write([]byte(field))
	}
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
