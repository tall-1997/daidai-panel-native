// Package trigticket 提供「可长期有效 + 绑定单个资源 + 可整批作废」的触发票据（issue #145，v3.3.2）。
//
// 它是 pkg/dlticket 的变体。两点不同让它没法直接复用那一份：
//
//  1. dlticket.Verify 强制校验过期（默认 TTL 只有 60 秒），而企业微信自建应用的菜单链接
//     是配一次用一年的 —— 点进去就过期的链接毫无意义；
//  2. 既然可以不过期，就必须有另一条止血路径：签名原文里带一个「代次」(generation)。
//     它存在 system_config 里，管理员点「作废全部链接」时 +1，所有旧票据立刻验不过，
//     不需要逐条记录已签发的票。
//
// 和 dlticket 一样，资源标识参与签名但**不随票据传输**：校验方必须自己算出同一个资源标识
// （这里是 `task-trigger:<任务 ID>`）才可能验签通过，所以一张票挪不到别的任务上。
//
// 安全取舍要写在这里，别让下一个人以为是疏忽：长期有效 = 拿到链接就等于拿到这一个任务的
// 永久触发权，链接会留在企业微信服务器、内置浏览器历史和反代访问日志里。所以
//   - 票据只绑一个任务（不是整个 tasks 权限）；
//   - 必须提供一键作废（代次 +1）；
//   - 调用方那一侧还压着「wecom_trigger_enabled + 允许触发执行」两级总开关。
package trigticket

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
	version   = "v1"
	separator = "."

	// domain 做域分隔。面板复用同一把 JWT secret 做多种签名（登录令牌、下载票据、触发票据），
	// 加上固定域前缀可以避免某一类签名被挪用到另一类上。
	// 这个串一旦改动，所有已发出去的链接立即失效，等同于一次全局作废。
	domain = "daidai-panel/task-trigger-ticket/v1"

	// neverExpires 是「不过期」在票据里的占位写法。刻意用 "0" 而不是省略这一段：
	// 字段数固定成 5 段，解析逻辑就不用分两种形态，也不会出现「少一段刚好绕过某个校验」。
	neverExpires = "0"
)

var (
	// ErrInvalid 表示票据格式错误、签名不匹配（含被篡改、资源不对应），或代次已被作废。
	ErrInvalid = errors.New("触发票据无效")
	// ErrExpired 表示签名有效但已过期（只有签发时显式给了 ttl 才可能出现）。
	ErrExpired = errors.New("触发票据已过期")
)

// Issue 为 resource 签发一张触发票据。
//
// ttl <= 0 表示永不过期（菜单链接的默认用法），此时返回的过期时间是零值。
// generation 是当前代次，由调用方从 system_config 读出来；作废全部链接就是把它 +1。
func Issue(secret, resource, subject string, generation int64, ttl time.Duration) (string, time.Time, error) {
	if strings.TrimSpace(secret) == "" {
		return "", time.Time{}, errors.New("签名密钥为空")
	}
	if strings.TrimSpace(resource) == "" {
		return "", time.Time{}, errors.New("资源标识为空")
	}

	exp := neverExpires
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
		exp = strconv.FormatInt(expiresAt.Unix(), 10)
	}

	encodedSubject := base64.RawURLEncoding.EncodeToString([]byte(subject))
	gen := strconv.FormatInt(generation, 10)

	ticket := strings.Join([]string{
		version,
		encodedSubject,
		exp,
		gen,
		sign(secret, resource, encodedSubject, exp, gen),
	}, separator)

	return ticket, expiresAt, nil
}

// Verify 校验票据是否为 resource 在当前代次下签发且仍然有效，成功时返回签发时的 subject。
//
// 代次不匹配一律按 ErrInvalid 处理，而不是单独给一个「已作废」错误：
// 对外多一种可区分的失败原因，就多一条给人探测的信息。
func Verify(secret, ticket, resource string, generation int64) (string, error) {
	if strings.TrimSpace(secret) == "" || strings.TrimSpace(resource) == "" {
		return "", ErrInvalid
	}

	parts := strings.Split(strings.TrimSpace(ticket), separator)
	if len(parts) != 5 || parts[0] != version {
		return "", ErrInvalid
	}
	encodedSubject, exp, gen, signature := parts[1], parts[2], parts[3], parts[4]

	// 代次直接参与签名原文：不用单独比一次，签名对不上就说明票是旧代次（或被篡改）的。
	expected := sign(secret, resource, encodedSubject, exp, strconv.FormatInt(generation, 10))
	if subtle.ConstantTimeCompare([]byte(signature), []byte(expected)) != 1 {
		return "", ErrInvalid
	}
	// 上面这一句已经把票里的 gen 间接校验过了，这里只做一次显式的格式检查，
	// 避免「签名碰巧相同但 gen 是垃圾字符」这种理论情况被放过。
	if _, err := strconv.ParseInt(gen, 10, 64); err != nil {
		return "", ErrInvalid
	}

	if exp != neverExpires {
		expUnix, err := strconv.ParseInt(exp, 10, 64)
		if err != nil {
			return "", ErrInvalid
		}
		if time.Now().After(time.Unix(expUnix, 0)) {
			return "", ErrExpired
		}
	}

	subject, err := base64.RawURLEncoding.DecodeString(encodedSubject)
	if err != nil {
		return "", ErrInvalid
	}
	return string(subject), nil
}

func sign(secret, resource, encodedSubject, exp, gen string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	// 每个字段带 8 字节大端长度前缀（与 pkg/dlticket 同一写法）：
	// 资源标识里含任务 ID，不加长度前缀的话不同字段组合有可能拼出同一段签名原文。
	for _, field := range []string{domain, resource, encodedSubject, exp, gen} {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(field)))
		mac.Write(length[:])
		mac.Write([]byte(field))
	}
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
