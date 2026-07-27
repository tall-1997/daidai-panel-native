package service

import (
	"fmt"
	"strings"
	"time"

	"sort"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/pkg/netutil"

	"gorm.io/gorm"
)

const (
	MaxLoginAttempts = 5
	LockDuration     = 15 * time.Minute
)

func RecordLoginLog(userID uint, username, ip, clientName, userAgent string, status int, message string) {
	log := model.LoginLog{
		UserID:     userID,
		Username:   username,
		IP:         ip,
		ClientName: clientName,
		UserAgent:  userAgent,
		Status:     status,
		Message:    message,
	}
	database.DB.Create(&log)
}

func CheckLoginLock(ip, username string) (bool, time.Duration) {
	var attempt model.LoginAttempt
	err := database.DB.Where("ip = ? AND username = ?", ip, username).
		Take(&attempt).Error

	if err != nil {
		return false, 0
	}

	if attempt.Count >= MaxLoginAttempts && attempt.LockedAt != nil {
		remaining := attempt.ExpiresAt.Sub(time.Now())
		if remaining > 0 {
			return true, remaining
		}
	}

	return false, 0
}

func GetLoginAttemptCount(ip, username string) int {
	var attempt model.LoginAttempt
	err := database.DB.Where("ip = ? AND username = ?", ip, username).
		Take(&attempt).Error
	if err != nil {
		return 0
	}
	return attempt.Count
}

func RecordFailedLogin(ip, username string) int {
	var attempt model.LoginAttempt
	err := database.DB.Where("ip = ? AND username = ?", ip, username).
		Take(&attempt).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			attempt = model.LoginAttempt{
				IP:        ip,
				Username:  username,
				Count:     1,
				ExpiresAt: time.Now().Add(LockDuration),
			}
			database.DB.Create(&attempt)
			return 1
		}
		return 0
	}

	attempt.Count++
	if attempt.Count >= MaxLoginAttempts {
		now := time.Now()
		attempt.LockedAt = &now
		lockTimes := attempt.Count - MaxLoginAttempts + 1
		attempt.ExpiresAt = now.Add(time.Duration(lockTimes) * LockDuration)
	}
	database.DB.Save(&attempt)
	return attempt.Count
}

func ClearLoginAttempts(ip, username string) {
	database.DB.Where("ip = ? AND username = ?", ip, username).Delete(&model.LoginAttempt{})
}

func CleanExpiredAttempts() {
	database.DB.Where("expires_at < ?", time.Now()).Delete(&model.LoginAttempt{})
}

func CreateSessionWithRefresh(userID uint, username, accessJTI, refreshJTI, clientType, clientName, ip, userAgent string, accessExpiresAt, refreshExpiresAt time.Time) {
	var refreshExpiryPtr *time.Time
	if !refreshExpiresAt.IsZero() {
		refreshExpiryPtr = &refreshExpiresAt
	}

	normalizedClientType := NormalizeSessionClientType(clientType)
	configKey := "max_web_sessions"
	if normalizedClientType == SessionClientApp {
		configKey = "max_app_sessions"
	}
	maxSessions := model.GetConfigInt(configKey, 1)
	if maxSessions < 1 {
		maxSessions = 1
	}
	revokeExcessSessionsByClientType(userID, normalizedClientType, maxSessions)

	session := model.UserSession{
		UserID:           userID,
		Username:         username,
		JTI:              accessJTI,
		RefreshJTI:       refreshJTI,
		ClientType:       normalizedClientType,
		ClientName:       clientName,
		IP:               ip,
		UserAgent:        userAgent,
		ExpiresAt:        accessExpiresAt,
		RefreshExpiresAt: refreshExpiryPtr,
	}
	database.DB.Create(&session)
}

func CreateSession(userID uint, username, jti, clientType, clientName, ip, userAgent string, expiresAt time.Time) {
	CreateSessionWithRefresh(userID, username, jti, "", clientType, clientName, ip, userAgent, expiresAt, time.Time{})
}

func effectiveSessionClientType(session model.UserSession) string {
	return DetectSessionClientType(session.ClientType, "", session.UserAgent)
}

func revokeExcessSessionsByClientType(userID uint, clientType string, maxSessions int) int64 {
	targetType := NormalizeSessionClientType(clientType)
	if maxSessions < 1 {
		maxSessions = 1
	}

	var sessions []model.UserSession
	database.DB.Where("user_id = ?", userID).Find(&sessions)

	var matched []model.UserSession
	for i := range sessions {
		if effectiveSessionClientType(sessions[i]) != targetType {
			continue
		}
		matched = append(matched, sessions[i])
	}

	// keep (maxSessions - 1) newest, revoke the rest (the new session will fill the last slot)
	keepCount := maxSessions - 1
	if len(matched) <= keepCount {
		return 0
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})

	toRevoke := matched[keepCount:]
	var ids []uint
	for i := range toRevoke {
		BlockSessionTokens(&toRevoke[i])
		ids = append(ids, toRevoke[i].ID)
	}

	if len(ids) == 0 {
		return 0
	}

	result := database.DB.Delete(&model.UserSession{}, ids)
	return result.RowsAffected
}

func blockToken(jti, tokenType string, userID *uint, expiresAt time.Time) {
	if jti == "" {
		return
	}
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(24 * time.Hour)
	}

	var existing model.TokenBlocklist
	if err := database.DB.Where("jti = ?", jti).First(&existing).Error; err == nil {
		return
	}

	database.DB.Create(&model.TokenBlocklist{
		JTI:       jti,
		TokenType: tokenType,
		UserID:    userID,
		RevokedAt: time.Now(),
		ExpiresAt: expiresAt,
	})
}

func BlockSessionTokens(session *model.UserSession) {
	if session == nil {
		return
	}
	userID := session.UserID
	blockToken(session.JTI, "access", &userID, session.ExpiresAt)
	if session.RefreshExpiresAt != nil {
		blockToken(session.RefreshJTI, "refresh", &userID, *session.RefreshExpiresAt)
	}
}

func RevokeSession(jti string) {
	var session model.UserSession
	if err := database.DB.Where("jti = ?", jti).First(&session).Error; err == nil {
		BlockSessionTokens(&session)
		database.DB.Delete(&session)
		return
	}

	blockToken(jti, "access", nil, time.Now().Add(24*time.Hour))
}

func RevokeAllUserSessions(userID uint) int64 {
	var sessions []model.UserSession
	database.DB.Where("user_id = ?", userID).Find(&sessions)
	for i := range sessions {
		BlockSessionTokens(&sessions[i])
	}

	result := database.DB.Where("user_id = ?", userID).Delete(&model.UserSession{})
	return result.RowsAffected
}

func RevokeOtherUserSessions(userID uint, currentJTI string) int64 {
	var sessions []model.UserSession
	database.DB.Where("user_id = ? AND jti != ?", userID, currentJTI).Find(&sessions)
	for i := range sessions {
		BlockSessionTokens(&sessions[i])
	}

	result := database.DB.Where("user_id = ? AND jti != ?", userID, currentJTI).Delete(&model.UserSession{})
	return result.RowsAffected
}

func CleanExpiredSessions() {
	now := time.Now()
	database.DB.Where("(refresh_expires_at IS NOT NULL AND refresh_expires_at < ?) OR (refresh_expires_at IS NULL AND expires_at < ?)", now, now).Delete(&model.UserSession{})
}

func IsIPWhitelisted(ip string) bool {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return false
	}

	var whitelist []model.IPWhitelist
	database.DB.Order("id ASC").Find(&whitelist)
	if len(whitelist) == 0 {
		return true
	}

	for _, entry := range whitelist {
		if netutil.MatchIPWhitelistEntry(entry.IP, ip) {
			return true
		}
	}

	return false
}

func RecordSecurityAudit(userID *uint, username, action, detail, ip string) {
	audit := model.SecurityAudit{
		UserID:   userID,
		Username: username,
		Action:   action,
		Detail:   detail,
		IP:       ip,
	}
	database.DB.Create(&audit)
}

func GetLoginStats(days int) map[string]interface{} {
	since := time.Now().AddDate(0, 0, -days)

	var totalLogins int64
	database.DB.Model(&model.LoginLog{}).Where("created_at > ?", since).Count(&totalLogins)

	var successLogins int64
	database.DB.Model(&model.LoginLog{}).Where("created_at > ? AND status = 0", since).Count(&successLogins)

	var failedLogins int64
	database.DB.Model(&model.LoginLog{}).Where("created_at > ? AND status = 1", since).Count(&failedLogins)

	var activeSessions int64
	database.DB.Model(&model.UserSession{}).Where("expires_at > ?", time.Now()).Count(&activeSessions)

	var lockedAccounts int64
	database.DB.Model(&model.LoginAttempt{}).
		Where("count >= ? AND expires_at > ?", MaxLoginAttempts, time.Now()).Count(&lockedAccounts)

	return map[string]interface{}{
		"total_logins":    totalLogins,
		"success_logins":  successLogins,
		"failed_logins":   failedLogins,
		"active_sessions": activeSessions,
		"locked_accounts": lockedAccounts,
		"period_days":     days,
	}
}

func IsSuspiciousLogin(ip, username string) (bool, string) {
	var lastLog model.LoginLog
	err := database.DB.Where("username = ? AND status = 0", username).
		Order("created_at DESC").Take(&lastLog).Error

	if err != nil {
		return false, ""
	}

	if lastLog.IP != ip {
		return true, fmt.Sprintf("IP 从 %s 变更为 %s", lastLog.IP, ip)
	}

	return false, ""
}
