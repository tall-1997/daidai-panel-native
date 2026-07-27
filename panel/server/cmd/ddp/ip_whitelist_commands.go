package main

import (
	"fmt"
	"strconv"
	"strings"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/pkg/netutil"

	"gorm.io/gorm"
)

func runIPWhitelist(rt *cliRuntime, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: ddp ip-whitelist <list|add|delete|clear|set> ...")
	}

	switch args[0] {
	case "list", "ls":
		return runIPWhitelistList(rt)
	case "add":
		return runIPWhitelistAdd(rt, args[1:])
	case "delete", "del", "remove", "rm":
		return runIPWhitelistDelete(rt, args[1:])
	case "clear", "reset":
		return runIPWhitelistClear(rt)
	case "set":
		return runIPWhitelistSet(rt, args[1:])
	default:
		return fmt.Errorf("未知 ip-whitelist 子命令: %s", args[0])
	}
}

func runIPWhitelistList(rt *cliRuntime) error {
	if err := rt.bootstrap(); err != nil {
		return err
	}

	var entries []model.IPWhitelist
	if err := database.DB.Order("id ASC").Find(&entries).Error; err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println("当前未设置 IP 白名单，所有 IP 均可访问登录页")
		return nil
	}

	for _, entry := range entries {
		fmt.Printf("[%d] %s", entry.ID, entry.IP)
		if strings.TrimSpace(entry.Remarks) != "" {
			fmt.Printf(" 备注=%s", entry.Remarks)
		}
		fmt.Println()
	}
	return nil
}

func runIPWhitelistAdd(rt *cliRuntime, args []string) error {
	if err := rt.bootstrap(); err != nil {
		return err
	}

	targets, remarks, err := parseIPWhitelistTargets(args, true)
	if err != nil {
		return err
	}

	created := 0
	for _, target := range targets {
		entry := model.IPWhitelist{
			IP:      target,
			Remarks: remarks,
		}
		if err := database.DB.Create(&entry).Error; err != nil {
			if isUniqueConstraintError(err) {
				return fmt.Errorf("IP 白名单已存在: %s", target)
			}
			return err
		}
		fmt.Printf("已添加 IP 白名单: %s (#%d)\n", entry.IP, entry.ID)
		created++
	}
	if created == 0 {
		fmt.Println("未添加任何 IP 白名单")
	}
	return nil
}

func runIPWhitelistDelete(rt *cliRuntime, args []string) error {
	if err := rt.bootstrap(); err != nil {
		return err
	}
	if len(args) != 1 {
		return fmt.Errorf("用法: ddp ip-whitelist delete <ID或IP/网段>")
	}

	target := strings.TrimSpace(args[0])
	query := database.DB.Model(&model.IPWhitelist{})
	if id, err := strconv.ParseUint(target, 10, 32); err == nil && id > 0 {
		query = query.Where("id = ?", id)
	} else {
		normalized, err := netutil.NormalizeIPWhitelistEntry(target)
		if err != nil {
			return err
		}
		query = query.Where("ip = ?", normalized)
	}

	result := query.Delete(&model.IPWhitelist{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("未找到匹配的 IP 白名单: %s", target)
	}

	fmt.Printf("已删除 %d 条 IP 白名单\n", result.RowsAffected)
	return nil
}

func runIPWhitelistClear(rt *cliRuntime) error {
	if err := rt.bootstrap(); err != nil {
		return err
	}

	result := database.DB.Where("1 = 1").Delete(&model.IPWhitelist{})
	if result.Error != nil {
		return result.Error
	}
	fmt.Printf("已清空 %d 条 IP 白名单；当前所有 IP 均可访问登录页\n", result.RowsAffected)
	return nil
}

func runIPWhitelistSet(rt *cliRuntime, args []string) error {
	if err := rt.bootstrap(); err != nil {
		return err
	}

	targets, remarks, err := parseIPWhitelistTargets(args, true)
	if err != nil {
		return err
	}

	return database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&model.IPWhitelist{}).Error; err != nil {
			return err
		}
		for _, target := range targets {
			entry := model.IPWhitelist{
				IP:      target,
				Remarks: remarks,
			}
			if err := tx.Create(&entry).Error; err != nil {
				return err
			}
		}
		fmt.Printf("已重设 IP 白名单，共 %d 条\n", len(targets))
		return nil
	})
}

func parseIPWhitelistTargets(args []string, requireTarget bool) ([]string, string, error) {
	remarks := ""
	rawTargets := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--remarks", "--remark", "-r":
			if i+1 >= len(args) {
				return nil, "", fmt.Errorf("%s 需要参数", args[i])
			}
			remarks = strings.TrimSpace(args[i+1])
			i++
		default:
			rawTargets = append(rawTargets, args[i])
		}
	}
	if requireTarget && len(rawTargets) == 0 {
		return nil, "", fmt.Errorf("请提供至少一个 IP、CIDR 或 IPv4 通配网段")
	}

	seen := make(map[string]bool, len(rawTargets))
	targets := make([]string, 0, len(rawTargets))
	for _, raw := range rawTargets {
		normalized, err := netutil.NormalizeIPWhitelistEntry(raw)
		if err != nil {
			return nil, "", err
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		targets = append(targets, normalized)
	}
	return targets, remarks, nil
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unique") || strings.Contains(text, "duplicate")
}
