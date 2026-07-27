package service

import "testing"

// installedProbe 构造一个假的“已安装探测器”，只把列出的版本视为已安装，
// 让回退逻辑单测无需真实 exec 探测系统 Python。
func installedProbe(versions ...string) func(string) bool {
	set := make(map[string]bool, len(versions))
	for _, v := range versions {
		set[v] = true
	}
	return func(v string) bool { return set[v] }
}

func TestResolvePythonFallbackVersion(t *testing.T) {
	supported := []string{"3.10", "3.11", "3.12"}

	t.Run("requested installed keeps requested and never falls back", func(t *testing.T) {
		got, fellBack := resolvePythonFallbackVersion("3.12", supported, installedProbe("3.11", "3.12"))
		if got != "3.12" || fellBack {
			t.Fatalf("expected 3.12 without fallback, got %q fellBack=%v", got, fellBack)
		}
	})

	t.Run("requested missing falls back to an installed supported version", func(t *testing.T) {
		got, fellBack := resolvePythonFallbackVersion("3.12", supported, installedProbe("3.11"))
		if got != "3.11" || !fellBack {
			t.Fatalf("expected fallback to 3.11, got %q fellBack=%v", got, fellBack)
		}
	})

	t.Run("nothing installed keeps requested without fallback", func(t *testing.T) {
		got, fellBack := resolvePythonFallbackVersion("3.12", supported, installedProbe())
		if got != "3.12" || fellBack {
			t.Fatalf("expected requested kept, got %q fellBack=%v", got, fellBack)
		}
	})

	t.Run("single runtime never falls back to another version", func(t *testing.T) {
		// Docker 固定版本：受支持集合只含一个版本，即便探测不到也不能回退到别的版本。
		got, fellBack := resolvePythonFallbackVersion("3.12", []string{"3.12"}, installedProbe())
		if got != "3.12" || fellBack {
			t.Fatalf("expected single-version runtime to keep 3.12, got %q fellBack=%v", got, fellBack)
		}
	})
}
