package wxcrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

// 企业微信官方文档「接收消息服务器配置」给出的验证 URL 示例。
// 这组串是离线验证这套算法唯一可用的手段（面板没法在测试里连企业微信），
// 所以这里当成固定向量断言：任何一步实现写错（排序漏了、iv 取错、填充按 16 去、
// 长度字段读成小端）都会让下面某一条挂掉。
const (
	sampleToken          = "QDG6eK"
	sampleEncodingAESKey = "jWmYm7qr5nMoAUwZRjGtBxmz3KA1tkAj3ykkR6q2B2C"
	sampleCorpID         = "wx5823bf96d3bd56c7"

	sampleMsgSignature = "5c45ff5e21c57e6ad56bac8758b79b1d9ac89fd3"
	sampleTimestamp    = "1409659589"
	sampleNonce        = "263014780"
	sampleEchostr      = "P9nAzCzyDtyTWESHep1vC5X9xho/qYX3Zpb4yKa9SKld1DsH3Iyt3tP3zNdtp+4RPcs8TgAE7OaBO+FZXvnaqQ=="
	sampleEchostrPlain = "1616140317555161061"
)

func TestSignatureMatchesOfficialSample(t *testing.T) {
	got := Signature(sampleToken, sampleTimestamp, sampleNonce, sampleEchostr)
	if got != sampleMsgSignature {
		t.Fatalf("msg_signature 与官方示例不一致\n  期望: %s\n  实际: %s", sampleMsgSignature, got)
	}
}

// TestSignatureIgnoresArgumentOrder 锁定「四个串排序后拼接」这一条：
// 实现里一旦改成按固定顺序拼接，这条会立刻挂。
func TestSignatureIgnoresArgumentOrder(t *testing.T) {
	a := Signature(sampleToken, sampleTimestamp, sampleNonce, sampleEchostr)
	b := Signature(sampleNonce, sampleEchostr, sampleToken, sampleTimestamp)
	if a != b {
		t.Fatalf("签名应当与参数顺序无关（协议是四个串排序后拼接）\n  %s\n  %s", a, b)
	}
}

func TestVerifySignature(t *testing.T) {
	if !VerifySignature(sampleToken, sampleTimestamp, sampleNonce, sampleEchostr, sampleMsgSignature) {
		t.Fatal("官方示例的签名应当校验通过")
	}
	// 只改最后一位：常量时间比较也必须判不通过。
	tampered := sampleMsgSignature[:len(sampleMsgSignature)-1] + "0"
	if tampered != sampleMsgSignature && VerifySignature(sampleToken, sampleTimestamp, sampleNonce, sampleEchostr, tampered) {
		t.Fatal("被篡改一位的签名不应通过")
	}
	if VerifySignature(sampleToken, sampleTimestamp, sampleNonce, sampleEchostr, "") {
		t.Fatal("空签名不应通过")
	}
}

func TestVerifyURLMatchesOfficialSample(t *testing.T) {
	plain, err := VerifyURL(sampleToken, sampleEncodingAESKey, sampleCorpID,
		sampleMsgSignature, sampleTimestamp, sampleNonce, sampleEchostr)
	if err != nil {
		t.Fatalf("官方示例应当验签并解密成功: %v", err)
	}
	if plain != sampleEchostrPlain {
		t.Fatalf("echostr 明文不一致\n  期望: %q\n  实际: %q", sampleEchostrPlain, plain)
	}
}

func TestVerifyURLRejectsBadSignature(t *testing.T) {
	_, err := VerifyURL(sampleToken, sampleEncodingAESKey, sampleCorpID,
		"0000000000000000000000000000000000000000", sampleTimestamp, sampleNonce, sampleEchostr)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("签名不对时应当返回 ErrInvalidSignature，实际: %v", err)
	}
}

// TestDecryptRejectsForeignReceiveID 是这个包最重要的一条：
// 密文本身完全合法（能解开），只是尾部的 receiveid 不是本企业的，必须判失败。
// 漏掉这一步 = 任何人拿自己企业的 Token/AESKey 就能触发面板任务执行。
func TestDecryptRejectsForeignReceiveID(t *testing.T) {
	_, err := Decrypt(sampleEncodingAESKey, "wx_someone_else", sampleEchostr)
	if !errors.Is(err, ErrReceiveIDMismatch) {
		t.Fatalf("receiveid 不匹配时应当返回 ErrReceiveIDMismatch，实际: %v", err)
	}

	// receiveID 传空串也必须判失败，不能退化成「不校验」。
	if _, err := Decrypt(sampleEncodingAESKey, "", sampleEchostr); !errors.Is(err, ErrReceiveIDMismatch) {
		t.Fatalf("receiveID 为空时应当返回 ErrReceiveIDMismatch，实际: %v", err)
	}
}

func TestDecryptRejectsBadInput(t *testing.T) {
	cases := []struct {
		name           string
		encodingAESKey string
		encrypt        string
		want           error
	}{
		{"AESKey 长度不对", "tooshort", sampleEchostr, ErrInvalidAESKey},
		{"AESKey 不是合法 base64", strings.Repeat("!", 43), sampleEchostr, ErrInvalidAESKey},
		{"密文不是合法 base64", sampleEncodingAESKey, "not-base64!!", ErrInvalidMessage},
		{"密文长度不是 16 的整数倍", sampleEncodingAESKey, base64.StdEncoding.EncodeToString([]byte("short")), ErrInvalidMessage},
		{"密文为空", sampleEncodingAESKey, "", ErrInvalidMessage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Decrypt(tc.encodingAESKey, sampleCorpID, tc.encrypt); !errors.Is(err, tc.want) {
				t.Fatalf("期望 %v，实际 %v", tc.want, err)
			}
		})
	}
}

// TestDecryptRoundTrip 用本地加密再解密走一遍完整报文（官方示例只覆盖了 echostr 那种短明文）。
// encryptForTest 是按协议现写的，与 Decrypt 互为镜像：任何一边的字段顺序或长度字段端序写反都过不了。
func TestDecryptRoundTrip(t *testing.T) {
	const msg = `<xml><ToUserName><![CDATA[wx5823bf96d3bd56c7]]></ToUserName><MsgType><![CDATA[text]]></MsgType><Content><![CDATA[运行 签到任务]]></Content></xml>`

	encrypted := encryptForTest(t, sampleEncodingAESKey, sampleCorpID, msg)
	plain, err := Decrypt(sampleEncodingAESKey, sampleCorpID, encrypted)
	if err != nil {
		t.Fatalf("往返解密失败: %v", err)
	}
	if string(plain) != msg {
		t.Fatalf("往返后明文不一致\n  期望: %q\n  实际: %q", msg, string(plain))
	}

	// 同一段明文换一个企业 ID 加密，就不该被本企业解出来。
	foreign := encryptForTest(t, sampleEncodingAESKey, "wx_someone_else", msg)
	if _, err := Decrypt(sampleEncodingAESKey, sampleCorpID, foreign); !errors.Is(err, ErrReceiveIDMismatch) {
		t.Fatalf("别的企业签出来的消息不应被接受，实际: %v", err)
	}
}

// encryptForTest 按企业微信的明文结构做一次加密，只给上面的往返用例用。
// 面板回包一律回空串（企业微信要求 5 秒内响应，结果走通知渠道异步推回），
// 所以生产代码里不需要加密能力，这段刻意只留在测试文件里。
func encryptForTest(t *testing.T, encodingAESKey, receiveID, msg string) string {
	t.Helper()

	key, err := decodeAESKey(encodingAESKey)
	if err != nil {
		t.Fatalf("decode aes key: %v", err)
	}

	var buf []byte
	// 随机串这里固定成 16 个 'a'：解密侧会整段丢掉，用例要的是可复现。
	buf = append(buf, []byte(strings.Repeat("a", randomPrefixLength))...)
	var length [msgLenFieldLength]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(msg)))
	buf = append(buf, length[:]...)
	buf = append(buf, []byte(msg)...)
	buf = append(buf, []byte(receiveID)...)

	pad := paddingBlockSize - len(buf)%paddingBlockSize
	for i := 0; i < pad; i++ {
		buf = append(buf, byte(pad))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	out := make([]byte, len(buf))
	cipher.NewCBCEncrypter(block, key[:aes.BlockSize]).CryptBlocks(out, buf)
	return base64.StdEncoding.EncodeToString(out)
}
