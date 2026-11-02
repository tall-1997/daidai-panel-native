package service

import (
	"strconv"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// 兜底值与区间必须和配置项的注册定义一致；数据库里存着历史越界值时回落到默认，不能把超时变成 0 或越界。
func TestDependencyOperationTimeoutFallsBackForOutOfRangeValues(t *testing.T) {
	testutil.SetupTestEnv(t)
	const key = "dependency_install_timeout_minutes"

	def, ok := model.GetSystemConfigDefinition(key)
	if !ok || def.Min == nil || def.Max == nil {
		t.Fatalf("expected %s registered with a range, got %+v (found=%v)", key, def, ok)
	}
	if want := strconv.Itoa(int(DefaultDependencyOperationTimeout / time.Minute)); def.DefaultValue != want {
		t.Fatalf("expected DefaultDependencyOperationTimeout (%s min) to match the registered default %q", want, def.DefaultValue)
	}
	if *def.Min != 5 || *def.Max != 720 {
		t.Fatalf("expected registered range 5..720 to match the fallback bounds, got %d..%d", *def.Min, *def.Max)
	}

	// 越界值绕过 SetConfig 的校验直接写库，模拟历史数据。
	setRaw := func(value string) {
		t.Helper()
		if err := database.DB.Where("`key` = ?", key).Delete(&model.SystemConfig{}).Error; err != nil {
			t.Fatalf("clear %s: %v", key, err)
		}
		if value == "" {
			return
		}
		if err := database.DB.Create(&model.SystemConfig{Key: key, Value: value}).Error; err != nil {
			t.Fatalf("write %s=%s: %v", key, value, err)
		}
	}
	cases := []struct {
		value string
		want  time.Duration
	}{
		{value: "", want: DefaultDependencyOperationTimeout},
		{value: "5", want: 5 * time.Minute},
		{value: "45", want: 45 * time.Minute},
		{value: "720", want: 720 * time.Minute},
		{value: "0", want: DefaultDependencyOperationTimeout},
		{value: "4", want: DefaultDependencyOperationTimeout},
		{value: "721", want: DefaultDependencyOperationTimeout},
	}
	for _, tc := range cases {
		setRaw(tc.value)
		if got := DependencyOperationTimeout(); got != tc.want {
			t.Fatalf("value %q: expected %s, got %s", tc.value, tc.want, got)
		}
	}
}
