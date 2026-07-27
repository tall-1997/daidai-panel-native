package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"daidai-panel/database"
	"daidai-panel/model"
)

var cliEnvNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func runEnv(rt *cliRuntime, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: ddp env <list|get|set|delete> ...")
	}

	switch args[0] {
	case "list":
		return runEnvList(rt, args[1:])
	case "get":
		return runEnvGet(rt, args[1:])
	case "set":
		return runEnvSet(rt, args[1:])
	case "delete":
		return runEnvDelete(rt, args[1:])
	default:
		return fmt.Errorf("未知 env 子命令: %s", args[0])
	}
}

func runEnvList(rt *cliRuntime, args []string) error {
	if err := rt.bootstrap(); err != nil {
		return err
	}

	group := ""
	keyword := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--group":
			if i+1 >= len(args) {
				return fmt.Errorf("--group 需要参数")
			}
			group = strings.TrimSpace(args[i+1])
			i++
		case "--keyword":
			if i+1 >= len(args) {
				return fmt.Errorf("--keyword 需要参数")
			}
			keyword = strings.TrimSpace(args[i+1])
			i++
		default:
			return fmt.Errorf("未知参数: %s", args[i])
		}
	}

	query := database.DB.Model(&model.EnvVar{}).
		Order("sort_order DESC, position ASC, created_at ASC, id ASC")
	if group != "" {
		if normalizedGroup := model.NormalizeEnvGroupValue(group); normalizedGroup != "" {
			query = query.Where("instr(',' || \"group\" || ',', ?) > 0", ","+normalizedGroup+",")
		}
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name LIKE ? OR remarks LIKE ?", like, like)
	}

	var envs []model.EnvVar
	if err := query.Find(&envs).Error; err != nil {
		return err
	}
	if len(envs) == 0 {
		fmt.Println("当前没有匹配的环境变量")
		return nil
	}

	for _, env := range envs {
		enabledText := boolLabel(env.Enabled, "启用", "禁用")
		groupText := env.Group
		if groupText == "" {
			groupText = "-"
		}
		fmt.Printf("[%d] %s 组=%s %s=%s\n", env.ID, enabledText, groupText, env.Name, truncateText(env.Value, 120))
		if strings.TrimSpace(env.Remarks) != "" {
			fmt.Printf("    备注: %s\n", env.Remarks)
		}
	}
	return nil
}

func runEnvGet(rt *cliRuntime, args []string) error {
	if err := rt.bootstrap(); err != nil {
		return err
	}
	if len(args) != 1 {
		return fmt.Errorf("用法: ddp env get <名称或ID>")
	}

	envs, err := findEnvVars(args[0])
	if err != nil {
		return err
	}

	for _, env := range envs {
		fmt.Printf("[%d] %s\n", env.ID, env.Name)
		fmt.Printf("  值: %s\n", env.Value)
		fmt.Printf("  状态: %s\n", boolLabel(env.Enabled, "启用", "禁用"))
		fmt.Printf("  分组: %s\n", emptyFallback(env.Group, "-"))
		fmt.Printf("  备注: %s\n", emptyFallback(env.Remarks, "-"))
	}
	return nil
}

func runEnvSet(rt *cliRuntime, args []string) error {
	if err := rt.bootstrap(); err != nil {
		return err
	}
	if len(args) < 2 {
		return fmt.Errorf("用法: ddp env set <名称> <值> [--group 分组] [--remarks 备注] [--disabled]")
	}

	name := strings.TrimSpace(args[0])
	value := args[1]
	group := ""
	remarks := ""
	enabled := true
	for i := 2; i < len(args); i++ {
		switch args[i] {
		case "--group":
			if i+1 >= len(args) {
				return fmt.Errorf("--group 需要参数")
			}
			group = strings.TrimSpace(args[i+1])
			i++
		case "--remarks":
			if i+1 >= len(args) {
				return fmt.Errorf("--remarks 需要参数")
			}
			remarks = args[i+1]
			i++
		case "--disabled":
			enabled = false
		default:
			return fmt.Errorf("未知参数: %s", args[i])
		}
	}

	if !cliEnvNamePattern.MatchString(name) {
		return fmt.Errorf("变量名格式无效: %s", name)
	}

	var matched []model.EnvVar
	query := database.DB.Where("name = ?", name)
	if remarks != "" {
		query = query.Where("remarks = ?", remarks)
	}
	if err := query.Order("id ASC").Find(&matched).Error; err != nil {
		return err
	}

	switch len(matched) {
	case 0:
		position, err := nextCLIEnvPosition()
		if err != nil {
			return err
		}
		env := model.EnvVar{
			Name:      name,
			Value:     value,
			Remarks:   remarks,
			Group:     model.NormalizeEnvGroupValue(group),
			Enabled:   enabled,
			SortOrder: 0,
			Position:  position,
		}
		if err := database.DB.Create(&env).Error; err != nil {
			return err
		}
		fmt.Printf("已创建环境变量: %s (#%d)\n", env.Name, env.ID)
	case 1:
		updates := map[string]interface{}{
			"value":   value,
			"group":   model.NormalizeEnvGroupValue(group),
			"remarks": remarks,
			"enabled": enabled,
		}
		if err := database.DB.Model(&matched[0]).Updates(updates).Error; err != nil {
			return err
		}
		fmt.Printf("已更新环境变量: %s (#%d)\n", matched[0].Name, matched[0].ID)
	default:
		return fmt.Errorf("存在多个同名环境变量，请提供 --remarks 精确定位，或改用面板处理")
	}

	return nil
}

func runEnvDelete(rt *cliRuntime, args []string) error {
	if err := rt.bootstrap(); err != nil {
		return err
	}
	if len(args) == 0 {
		return fmt.Errorf("用法: ddp env delete <名称或ID> [--all]")
	}

	all := false
	target := ""
	for _, arg := range args {
		if arg == "--all" {
			all = true
			continue
		}
		if target != "" {
			return fmt.Errorf("只能指定一个名称或 ID")
		}
		target = arg
	}
	if target == "" {
		return fmt.Errorf("缺少要删除的环境变量")
	}

	envs, err := findEnvVars(target)
	if err != nil {
		return err
	}
	if len(envs) > 1 && !all {
		return fmt.Errorf("存在多个匹配项，请加 --all 或改用 ID")
	}

	ids := make([]uint, 0, len(envs))
	for _, env := range envs {
		ids = append(ids, env.ID)
	}
	if !all && len(ids) > 1 {
		ids = ids[:1]
	}

	result := database.DB.Where("id IN ?", ids).Delete(&model.EnvVar{})
	if result.Error != nil {
		return result.Error
	}
	fmt.Printf("已删除 %d 个环境变量\n", result.RowsAffected)
	return nil
}

func nextCLIEnvPosition() (float64, error) {
	var last model.EnvVar
	err := database.DB.Where("sort_order = ?", 0).
		Order("position DESC, id DESC").
		First(&last).Error
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "record not found") {
			return 1000, nil
		}
		return 0, err
	}
	return last.Position + 1000, nil
}

func findEnvVars(identifier string) ([]model.EnvVar, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("环境变量标识不能为空")
	}

	if id, err := strconv.ParseUint(identifier, 10, 32); err == nil {
		var env model.EnvVar
		if err := database.DB.First(&env, id).Error; err != nil {
			return nil, fmt.Errorf("环境变量不存在: %s", identifier)
		}
		return []model.EnvVar{env}, nil
	}

	var envs []model.EnvVar
	if err := database.DB.Where("name = ?", identifier).Order("id ASC").Find(&envs).Error; err != nil {
		return nil, err
	}
	if len(envs) == 0 {
		return nil, fmt.Errorf("环境变量不存在: %s", identifier)
	}
	return envs, nil
}

func emptyFallback(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
