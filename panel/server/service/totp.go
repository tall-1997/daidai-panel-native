package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	TOTPDigits = 6
	TOTPPeriod = 30
	TOTPIssuer = "DaiDaiPanel"
)

func silentDB() *gorm.DB {
	return database.DB.Session(&gorm.Session{Logger: database.DB.Logger.LogMode(logger.Silent)})
}

func GenerateTOTPSecret() string {
	secret := make([]byte, 20)
	rand.Read(secret)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
}

func GenerateTOTPURI(username, secret string) string {
	return fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&digits=%d&period=%d",
		TOTPIssuer, username, secret, TOTPIssuer, TOTPDigits, TOTPPeriod)
}

// GenerateCurrentTOTPForTest returns a currently-valid 6-digit TOTP code for
// the supplied base32 secret. Exported so end-to-end tests can exercise the
// 2FA flows without re-implementing the HMAC-SHA1 code generation.
func GenerateCurrentTOTPForTest(secret string) string {
	counter := uint64(time.Now().Unix() / int64(TOTPPeriod))
	return generateTOTPCode(secret, counter)
}

func ValidateTOTP(secret, code string) bool {
	now := time.Now().Unix()
	for _, offset := range []int64{-1, 0, 1} {
		counter := uint64((now / int64(TOTPPeriod)) + offset)
		generated := generateTOTPCode(secret, counter)
		if generated == code {
			return true
		}
	}
	return false
}

func generateTOTPCode(secret string, counter uint64) string {
	secretBytes, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return ""
	}

	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)

	mac := hmac.New(sha1.New, secretBytes)
	mac.Write(buf)
	hash := mac.Sum(nil)

	offset := hash[len(hash)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(hash[offset:offset+4]) & 0x7fffffff

	code := truncated % 1000000
	return fmt.Sprintf("%06d", code)
}

func SetupTwoFactor(userID uint) (string, string, error) {
	secret := GenerateTOTPSecret()

	var existing model.TwoFactorAuth
	err := silentDB().Where("user_id = ?", userID).First(&existing).Error
	if err == nil {
		// 这条分支才是「重新设置 2FA」的常走路径（库里已经有 two_factor_auths 行）。
		// 和下面的 Create 一样必须接 .Error：写库失败时如果照样往下走，
		// 函数会把**新** secret 和二维码返回给前端，而库里存的还是**旧** secret，
		// 用户扫完码输入的验证码永远对不上，且没有任何线索能看出发生了什么。
		if err := database.DB.Model(&existing).Updates(map[string]interface{}{
			"secret":  secret,
			"enabled": false,
		}).Error; err != nil {
			return "", "", err
		}
	} else {
		tfa := model.TwoFactorAuth{
			UserID:  userID,
			Secret:  secret,
			Enabled: false,
		}
		// 原来这行没接 .Error：two_factor_auths.user_id 是唯一索引，重复/并发的开启请求
		// 会撞唯一约束而插入失败，却被静默吞掉 —— 用户扫完二维码怎么都验证不过，
		// 因为库里压根没有这条 secret。这里把错误抛给 handler，由它统一返回「设置 2FA 失败」。
		if err := database.DB.Create(&tfa).Error; err != nil {
			return "", "", err
		}
	}

	var user model.User
	database.DB.First(&user, userID)
	uri := GenerateTOTPURI(user.Username, secret)

	return secret, uri, nil
}

func VerifyAndEnableTwoFactor(userID uint, code string) error {
	var tfa model.TwoFactorAuth
	if err := silentDB().Where("user_id = ?", userID).First(&tfa).Error; err != nil {
		return fmt.Errorf("2FA 尚未设置")
	}

	if !ValidateTOTP(tfa.Secret, code) {
		return fmt.Errorf("验证码无效")
	}

	database.DB.Model(&tfa).Update("enabled", true)
	return nil
}

func DisableTwoFactor(userID uint) {
	database.DB.Where("user_id = ?", userID).Delete(&model.TwoFactorAuth{})
}

func IsTwoFactorEnabled(userID uint) bool {
	var tfa model.TwoFactorAuth
	err := silentDB().Where("user_id = ? AND enabled = ?", userID, true).First(&tfa).Error
	return err == nil
}

func ValidateUserTOTP(userID uint, code string) bool {
	var tfa model.TwoFactorAuth
	if err := silentDB().Where("user_id = ? AND enabled = ?", userID, true).First(&tfa).Error; err != nil {
		return false
	}
	return ValidateTOTP(tfa.Secret, code)
}
