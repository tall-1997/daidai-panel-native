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
		database.DB.Model(&existing).Updates(map[string]interface{}{
			"secret":  secret,
			"enabled": false,
		})
	} else {
		tfa := model.TwoFactorAuth{
			UserID:  userID,
			Secret:  secret,
			Enabled: false,
		}
		database.DB.Create(&tfa)
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
