package cron

import (
	"fmt"
	"sort"
	"strings"
	"time"

	robfigcron "github.com/robfig/cron/v3"
)

type ParseResult struct {
	Valid       bool
	HasSecond   bool
	Fields      map[string]string
	Description string
	Error       string
}

func SplitExpressions(raw string) []string {
	lines := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r'
	})
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func NormalizeExpressions(raw string) string {
	return strings.Join(SplitExpressions(raw), "\n")
}

func ValidateExpressions(raw string) error {
	expressions := SplitExpressions(raw)
	if len(expressions) == 0 {
		return fmt.Errorf("请至少填写一条定时规则")
	}

	for index, expression := range expressions {
		result := Parse(expression)
		if !result.Valid {
			return fmt.Errorf("第 %d 条定时规则无效: %s", index+1, result.Error)
		}
	}
	return nil
}

func Parse(expression string) ParseResult {
	expression = strings.TrimSpace(expression)
	parts := strings.Fields(expression)

	parser, hasSecond, err := parserForParts(parts)
	if err != nil {
		return ParseResult{Valid: false, Error: err.Error()}
	}

	if _, err := parser.Parse(expression); err != nil {
		return ParseResult{Valid: false, Error: err.Error()}
	}

	fields := buildFields(parts, hasSecond)
	return ParseResult{
		Valid:       true,
		HasSecond:   hasSecond,
		Fields:      fields,
		Description: describe(fields, hasSecond),
	}
}

func NextRunTimes(expression string, count int) []time.Time {
	return NextRunTimesFrom(expression, count, time.Now())
}

func NextRunTimesFrom(expression string, count int, from time.Time) []time.Time {
	if count <= 0 {
		return nil
	}

	schedule, err := ParseSchedule(expression)
	if err != nil {
		return nil
	}

	times := make([]time.Time, 0, count)
	next := from
	for i := 0; i < count; i++ {
		next = schedule.Next(next)
		if next.IsZero() {
			break
		}
		times = append(times, next)
	}
	return times
}

func NextRunTimesForExpressions(raw string, count int) []time.Time {
	return NextRunTimesForExpressionsFrom(raw, count, time.Now())
}

func NextRunTimesForExpressionsFrom(raw string, count int, from time.Time) []time.Time {
	if count <= 0 {
		return nil
	}

	expressions := SplitExpressions(raw)
	if len(expressions) == 0 {
		return nil
	}

	times := make([]time.Time, 0, len(expressions)*count)
	for _, expression := range expressions {
		times = append(times, NextRunTimesFrom(expression, count, from)...)
	}

	sort.Slice(times, func(i, j int) bool {
		return times[i].Before(times[j])
	})

	if len(times) > count {
		times = times[:count]
	}
	return times
}

func parserForParts(parts []string) (robfigcron.Parser, bool, error) {
	switch len(parts) {
	case 5:
		return robfigcron.NewParser(
			robfigcron.Minute |
				robfigcron.Hour |
				robfigcron.Dom |
				robfigcron.Month |
				robfigcron.Dow |
				robfigcron.Descriptor,
		), false, nil
	case 6:
		return robfigcron.NewParser(
			robfigcron.Second |
				robfigcron.Minute |
				robfigcron.Hour |
				robfigcron.Dom |
				robfigcron.Month |
				robfigcron.Dow |
				robfigcron.Descriptor,
		), true, nil
	default:
		return robfigcron.Parser{}, false, errInvalidFieldCount
	}
}

func parseSchedule(expression string) (robfigcron.Schedule, error) {
	return ParseSchedule(expression)
}

func ParseSchedule(expression string) (robfigcron.Schedule, error) {
	expression = strings.TrimSpace(expression)
	parts := strings.Fields(expression)
	parser, _, err := parserForParts(parts)
	if err != nil {
		return nil, err
	}
	return parser.Parse(expression)
}

var errInvalidFieldCount = &parseError{message: "cron expression must have 5 or 6 fields"}

type parseError struct {
	message string
}

func (e *parseError) Error() string {
	return e.message
}

func buildFields(parts []string, hasSecond bool) map[string]string {
	if hasSecond {
		return map[string]string{
			"second":      parts[0],
			"minute":      parts[1],
			"hour":        parts[2],
			"day":         parts[3],
			"month":       parts[4],
			"day_of_week": parts[5],
		}
	}

	return map[string]string{
		"minute":      parts[0],
		"hour":        parts[1],
		"day":         parts[2],
		"month":       parts[3],
		"day_of_week": parts[4],
	}
}

func describe(fields map[string]string, hasSecond bool) string {
	if hasSecond {
		if desc, ok := describeSimpleStep(fields["second"], "秒"); ok {
			return desc
		}
	}
	if desc, ok := describeSimpleStep(fields["minute"], "分钟"); ok {
		return desc
	}
	if desc, ok := describeSimpleStep(fields["hour"], "小时"); ok {
		return desc
	}

	minute := fields["minute"]
	hour := fields["hour"]
	day := fields["day"]
	month := normalizeMonth(fields["month"])
	dow := normalizeWeek(fields["day_of_week"])

	if isEvery(month) && isEvery(day) && isEvery(hour) && isEvery(minute) {
		return "每分钟"
	}
	if isEvery(month) && isEvery(day) && hour == "0" && minute == "0" {
		return "每天 00:00"
	}
	if isEvery(month) && isEvery(day) && isNumeric(hour) && isNumeric(minute) {
		return "每天 " + twoDigits(hour) + ":" + twoDigits(minute)
	}
	if isEvery(month) && day == "*" && !isEvery(dow) && isNumeric(hour) && isNumeric(minute) {
		return "每周 " + dow + " " + twoDigits(hour) + ":" + twoDigits(minute)
	}
	if month != "*" && day != "*" && isNumeric(hour) && isNumeric(minute) {
		return "每年 " + month + " " + day + "日 " + twoDigits(hour) + ":" + twoDigits(minute)
	}
	if day != "*" && isNumeric(hour) && isNumeric(minute) {
		return "每月 " + day + "日 " + twoDigits(hour) + ":" + twoDigits(minute)
	}
	return "自定义 cron 表达式"
}

func describeSimpleStep(field, unit string) (string, bool) {
	if strings.HasPrefix(field, "*/") {
		return "每" + strings.TrimPrefix(field, "*/") + unit, true
	}
	return "", false
}

func normalizeWeek(value string) string {
	upper := strings.ToUpper(strings.TrimSpace(value))
	replacer := strings.NewReplacer(
		"SUN", "周日",
		"MON", "周一",
		"TUE", "周二",
		"WED", "周三",
		"THU", "周四",
		"FRI", "周五",
		"SAT", "周六",
		"0", "周日",
		"1", "周一",
		"2", "周二",
		"3", "周三",
		"4", "周四",
		"5", "周五",
		"6", "周六",
		"7", "周日",
	)
	return replacer.Replace(upper)
}

func normalizeMonth(value string) string {
	upper := strings.ToUpper(strings.TrimSpace(value))
	replacer := strings.NewReplacer(
		"JAN", "1月",
		"FEB", "2月",
		"MAR", "3月",
		"APR", "4月",
		"MAY", "5月",
		"JUN", "6月",
		"JUL", "7月",
		"AUG", "8月",
		"SEP", "9月",
		"OCT", "10月",
		"NOV", "11月",
		"DEC", "12月",
	)
	return replacer.Replace(upper)
}

func isEvery(value string) bool {
	return value == "*" || value == "?"
}

func isNumeric(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func twoDigits(value string) string {
	if len(value) == 1 {
		return "0" + value
	}
	return value
}

func GetTemplates() []map[string]string {
	return []map[string]string{
		{"name": "每分钟", "expression": "0 * * * * *", "description": "每分钟执行一次", "category": "高频"},
		{"name": "每5分钟", "expression": "0 */5 * * * *", "description": "每5分钟执行一次", "category": "高频"},
		{"name": "每10分钟", "expression": "0 */10 * * * *", "description": "每10分钟执行一次", "category": "高频"},
		{"name": "每15分钟", "expression": "0 */15 * * * *", "description": "每15分钟执行一次", "category": "高频"},
		{"name": "每30分钟", "expression": "0 */30 * * * *", "description": "每30分钟执行一次", "category": "常用"},
		{"name": "每小时", "expression": "0 0 * * * *", "description": "每小时整点执行", "category": "常用"},
		{"name": "每2小时", "expression": "0 0 */2 * * *", "description": "每2小时执行一次", "category": "常用"},
		{"name": "每6小时", "expression": "0 0 */6 * * *", "description": "每6小时执行一次", "category": "常用"},
		{"name": "每天0点", "expression": "0 0 0 * * *", "description": "每天凌晨0点执行", "category": "每天"},
		{"name": "每天6点", "expression": "0 0 6 * * *", "description": "每天早上6点执行", "category": "每天"},
		{"name": "每天9点", "expression": "0 0 9 * * *", "description": "每天上午9点执行", "category": "每天"},
		{"name": "每天12点", "expression": "0 0 12 * * *", "description": "每天中午12点执行", "category": "每天"},
		{"name": "每天18点", "expression": "0 0 18 * * *", "description": "每天下午6点执行", "category": "每天"},
		{"name": "工作日9点", "expression": "0 0 9 * * 1-5", "description": "工作日上午9点执行", "category": "工作日"},
		{"name": "工作日18点", "expression": "0 0 18 * * 1-5", "description": "工作日下午6点执行", "category": "工作日"},
		{"name": "周末10点", "expression": "0 0 10 * * 0,6", "description": "周末上午10点执行", "category": "周末"},
		{"name": "每周一0点", "expression": "0 0 0 * * 1", "description": "每周一凌晨0点执行", "category": "每周"},
		{"name": "每月1日0点", "expression": "0 0 0 1 * *", "description": "每月1日凌晨0点执行", "category": "每月"},
		{"name": "每月15日0点", "expression": "0 0 0 15 * *", "description": "每月15日凌晨0点执行", "category": "每月"},
		{"name": "每10秒", "expression": "*/10 * * * * *", "description": "每10秒执行一次", "category": "秒级"},
		{"name": "每30秒", "expression": "*/30 * * * * *", "description": "每30秒执行一次", "category": "秒级"},
	}
}
