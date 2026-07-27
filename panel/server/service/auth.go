package service

import (
	"errors"
	"strings"
	"time"

	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/pkg/crypto"
	"daidai-panel/pkg/validator"
)

var (
	ErrUserNotFound     = errors.New("用户不存在")
	ErrInvalidPassword  = errors.New("密码错误")
	ErrUserDisabled     = errors.New("账号已被禁用")
	ErrUserExists       = errors.New("用户名已存在")
	ErrInvalidUsername  = errors.New("用户名格式无效")
	ErrPasswordTooShort = errors.New("密码过短")
	ErrTOTPRequired     = errors.New("需要两步验证码")
	ErrInvalidTOTP      = errors.New("两步验证码错误")
)

type AuthService struct{}

func NewAuthService() *AuthService {
	return &AuthService{}
}

func (s *AuthService) NeedInit() bool {
	var count int64
	database.DB.Model(&model.User{}).Count(&count)
	return count == 0
}

func (s *AuthService) InitAdmin(username, password string) (*model.User, error) {
	if !s.NeedInit() {
		return nil, errors.New("系统已初始化")
	}

	if !validator.ValidateUsername(username) {
		return nil, ErrInvalidUsername
	}
	if !validator.ValidatePassword(password) {
		return nil, ErrPasswordTooShort
	}

	hash, err := crypto.HashPassword(password)
	if err != nil {
		return nil, err
	}

	user := &model.User{
		Username: username,
		Password: hash,
		Role:     "admin",
		Enabled:  true,
	}
	if err := database.DB.Create(user).Error; err != nil {
		return nil, err
	}
	return user, nil
}

func (s *AuthService) Login(username, password, totpCode string) (*model.User, string, string, *middleware.TokenInfo, *middleware.TokenInfo, error) {
	username = validator.SanitizeString(username)
	totpCode = strings.TrimSpace(totpCode)

	var user model.User
	if err := database.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, "", "", nil, nil, ErrUserNotFound
	}

	if !user.Enabled {
		return nil, "", "", nil, nil, ErrUserDisabled
	}

	if !crypto.CheckPassword(password, user.Password) {
		return nil, "", "", nil, nil, ErrInvalidPassword
	}

	if IsTwoFactorEnabled(user.ID) {
		if totpCode == "" {
			return nil, "", "", nil, nil, ErrTOTPRequired
		}
		if !ValidateUserTOTP(user.ID, totpCode) {
			return nil, "", "", nil, nil, ErrInvalidTOTP
		}
	}

	now := time.Now()
	user.LastLoginAt = &now
	database.DB.Save(&user)

	tokenInfo, err := middleware.GenerateAccessTokenInfo(user.Username, user.Role)
	if err != nil {
		return nil, "", "", nil, nil, err
	}

	refreshInfo, err := middleware.GenerateRefreshTokenInfo(user.Username, user.Role)
	if err != nil {
		return nil, "", "", nil, nil, err
	}

	return &user, tokenInfo.Token, refreshInfo.Token, tokenInfo, refreshInfo, nil
}

func (s *AuthService) RefreshToken(tokenStr string) (string, error) {
	claims, err := middleware.ParseToken(tokenStr)
	if err != nil {
		return "", errors.New("刷新令牌无效")
	}

	if claims.TokenType != "refresh" {
		return "", errors.New("不是刷新令牌")
	}

	if middleware.IsTokenBlocked(claims.ID) {
		return "", errors.New("刷新令牌已被撤销")
	}

	var user model.User
	if err := database.DB.Where("username = ?", claims.Username).First(&user).Error; err != nil {
		return "", ErrUserNotFound
	}

	if !user.Enabled {
		return "", ErrUserDisabled
	}

	return middleware.GenerateAccessToken(user.Username, user.Role)
}

func (s *AuthService) Logout(jti string, userID *uint) error {
	blocked := model.TokenBlocklist{
		JTI:       jti,
		TokenType: "access",
		UserID:    userID,
		RevokedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	return database.DB.Create(&blocked).Error
}

func (s *AuthService) GetUser(username string) (*model.User, error) {
	var user model.User
	if err := database.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, ErrUserNotFound
	}
	return &user, nil
}

func (s *AuthService) ChangePassword(username, oldPassword, newPassword string) error {
	var user model.User
	if err := database.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return ErrUserNotFound
	}

	if !crypto.CheckPassword(oldPassword, user.Password) {
		return ErrInvalidPassword
	}

	if !validator.ValidatePassword(newPassword) {
		return ErrPasswordTooShort
	}

	hash, err := crypto.HashPassword(newPassword)
	if err != nil {
		return err
	}

	return database.DB.Model(&user).Update("password", hash).Error
}
