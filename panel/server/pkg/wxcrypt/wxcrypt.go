// Package wxcrypt 实现企业微信「接收消息服务器」的签名校验与消息解密（issue #145，v3.3.2）。
//
// 为什么要自己写一份：企业微信这套算法是 AES-256-CBC + 自定义 32 字节对齐填充 + 裸 sha1 签名，
// 和面板已有的两处加密都对不上 —— service/backup.go 是 AES-GCM，pkg/dlticket 是 HMAC-SHA256；
// 全仓 grep wxbizmsgcrypt / EncodingAESKey / NewCBCDecrypter 零命中，没有可复用的代码。
// 为两百行算法引一个第三方依赖不划算（go.mod 目前没有任何微信相关依赖）。
//
// 安全上有两条硬约束，少任何一条这条公网回调就变成「任何人都能触发任务执行」：
//  1. msg_signature 必须用常量时间比较（VerifySignature），不能用 `!=`；
//  2. 解密后明文尾部的 receiveid 必须与本企业的 CorpID 相同（Decrypt 里强制校验，
//     没有跳过这一步的开关）—— 否则别人拿自己企业的 Token/AESKey 就能构造出一条
//     签名合法的消息打进来。
package wxcrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
)

const (
	// encodingAESKeyLength 是企业微信后台给出的 EncodingAESKey 长度。
	// 它是「去掉末尾那个 = 的标准 base64」，补回去解出来正好 32 字节（AES-256）。
	encodingAESKeyLength = 43
	// aesKeySize 是 AES-256 的密钥长度。
	aesKeySize = 32
	// paddingBlockSize 是企业微信填充用的块长。注意它是 32 而不是 AES 的 16，
	// 所以去填充时合法的填充长度范围是 1-32，不能照抄常见的 1-16 写法。
	paddingBlockSize = 32
	// randomPrefixLength 是明文头部那段无意义的随机串长度。
	randomPrefixLength = 16
	// msgLenFieldLength 是紧跟随机串的 4 字节大端消息长度。
	msgLenFieldLength = 4
)

var (
	// ErrInvalidAESKey 表示 EncodingAESKey 长度不对或不是合法 base64。
	ErrInvalidAESKey = errors.New("EncodingAESKey 无效")
	// ErrInvalidSignature 表示 msg_signature 对不上。
	ErrInvalidSignature = errors.New("企业微信消息签名校验失败")
	// ErrInvalidMessage 表示密文格式不合法（长度不对齐、填充异常、长度字段越界等）。
	ErrInvalidMessage = errors.New("企业微信消息密文无效")
	// ErrReceiveIDMismatch 表示解密成功但消息不是发给本企业的。
	ErrReceiveIDMismatch = errors.New("企业微信消息不属于本企业")
)

// Signature 按企业微信的规定算 msg_signature：token / timestamp / nonce / encrypt
// 四个串字典序排序后直接拼接，取 sha1 的十六进制小写。
//
// 注意排序对象是「四个串」而不是「四个键值对」，也不带任何分隔符，这是协议规定的，
// 看着别扭但不能改成更直观的写法。
func Signature(token, timestamp, nonce, encrypt string) string {
	parts := []string{token, timestamp, nonce, encrypt}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(sum[:])
}

// VerifySignature 用常量时间比较校验 msg_signature。
//
// 刻意不用 `!=`：签名是这条公网入口上唯一的身份凭证，逐字节短路比较会从响应耗时上
// 泄漏「已经对上了几位」，把离线爆破变成在线爆破。面板另一处 handler/open_api.go 的
// 裸 `!=` 是历史遗留，新代码一律照 handler/mcp_auth.go 的 ConstantTimeCompare 写。
func VerifySignature(token, timestamp, nonce, encrypt, msgSignature string) bool {
	expected := Signature(token, timestamp, nonce, encrypt)
	got := strings.TrimSpace(msgSignature)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(got)) == 1
}

// VerifyURL 处理企业微信后台保存回调配置时发来的那一次 GET 验签，
// 返回解密后的 echostr 明文 —— 调用方必须把它原样回给企业微信（纯文本，不要包 JSON）。
func VerifyURL(token, encodingAESKey, receiveID, msgSignature, timestamp, nonce, echostr string) (string, error) {
	if !VerifySignature(token, timestamp, nonce, echostr, msgSignature) {
		return "", ErrInvalidSignature
	}
	plain, err := Decrypt(encodingAESKey, receiveID, echostr)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// Decrypt 解出企业微信密文里的业务报文。
//
// 明文结构：random(16) | msgLen(4, 大端) | msg | receiveid
// 其中 receiveid 在企业微信自建应用场景下就是 CorpID，必须与 receiveID 参数一致。
func Decrypt(encodingAESKey, receiveID, encrypt string) ([]byte, error) {
	// receiveID 为空时无从校验归属，直接判失败：绝不能退化成「不校验」。
	if strings.TrimSpace(receiveID) == "" {
		return nil, ErrReceiveIDMismatch
	}

	key, err := decodeAESKey(encodingAESKey)
	if err != nil {
		return nil, err
	}

	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encrypt))
	if err != nil {
		return nil, ErrInvalidMessage
	}
	if len(ciphertext) < aes.BlockSize || len(ciphertext)%aes.BlockSize != 0 {
		return nil, ErrInvalidMessage
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrInvalidAESKey
	}
	// iv 不随密文传输，企业微信规定取 AESKey 的前 16 字节。
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, key[:aes.BlockSize]).CryptBlocks(plain, ciphertext)

	plain, err = stripPadding(plain)
	if err != nil {
		return nil, err
	}
	if len(plain) < randomPrefixLength+msgLenFieldLength {
		return nil, ErrInvalidMessage
	}

	body := plain[randomPrefixLength:]
	msgLen := binary.BigEndian.Uint32(body[:msgLenFieldLength])
	body = body[msgLenFieldLength:]
	// 长度字段来自密文内部、完全受攻击者控制，先用 uint64 比一次再转 int，
	// 免得 32 位平台上 int(msgLen) 溢出成负数绕过下面的切片边界。
	if uint64(msgLen) > uint64(len(body)) {
		return nil, ErrInvalidMessage
	}

	msg := body[:msgLen]
	gotReceiveID := body[msgLen:]
	if subtle.ConstantTimeCompare(gotReceiveID, []byte(receiveID)) != 1 {
		return nil, ErrReceiveIDMismatch
	}
	return msg, nil
}

func decodeAESKey(encodingAESKey string) ([]byte, error) {
	encodingAESKey = strings.TrimSpace(encodingAESKey)
	if len(encodingAESKey) != encodingAESKeyLength {
		return nil, ErrInvalidAESKey
	}
	// 企业微信给的是掐掉末位 '=' 的标准 base64，补回来才能解。
	key, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil || len(key) != aesKeySize {
		return nil, ErrInvalidAESKey
	}
	return key, nil
}

// stripPadding 去掉企业微信那套 PKCS#7 填充。它按 32 字节对齐（不是 AES 的 16），
// 所以填充长度的合法范围是 1-32。填充值异常时按「密文无效」处理，不做任何容错 ——
// 这里的输入全部来自公网，容错等于给构造畸形包留口子。
func stripPadding(plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return nil, ErrInvalidMessage
	}
	pad := int(plain[len(plain)-1])
	if pad < 1 || pad > paddingBlockSize || pad > len(plain) {
		return nil, ErrInvalidMessage
	}
	return plain[:len(plain)-pad], nil
}
