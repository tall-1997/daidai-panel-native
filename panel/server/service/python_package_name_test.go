package service

import (
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestCanonicalizePythonPackageName(t *testing.T) {
	cases := map[string]string{
		"requests":                     "requests",
		"Requests":                     "requests",
		"  REQUESTS  ":                 "requests",
		"Flask_SQLAlchemy":             "flask-sqlalchemy",
		"flask-sqlalchemy":             "flask-sqlalchemy",
		"zope.interface":               "zope-interface",
		"requests==2.31.0":             "requests",
		"requests>=2.0":                "requests",
		"requests~=2.31.0":             "requests",
		"requests!=2.0":                "requests",
		"requests<3":                   "requests",
		"requests[security]":           "requests",
		"zope.interface[test]":         "zope-interface",
		"requests; python_version<'3'": "requests",
		"requests == 2.31.0":           "requests",
		"":                             "",
		"   ":                          "",
	}

	for input, expected := range cases {
		if got := CanonicalizePythonPackageName(input); got != expected {
			t.Errorf("CanonicalizePythonPackageName(%q) = %q, want %q", input, got, expected)
		}
	}
}

// issue #166：判断旧写法的 extras 是否已覆盖新写法，决定是「已存在跳过」还是「原地改名重装」。
func TestPythonExtrasCovered(t *testing.T) {
	cases := []struct {
		existing  string
		requested string
		want      bool
	}{
		// 新写法没有 extras：任何旧写法都算覆盖，照旧跳过。
		{"httpx", "httpx", true},
		{"httpx[http2]", "httpx", true},
		{"httpx==0.28.1", "httpx", true},
		// 报告里的场景：已装 httpx，再装 httpx[http2] 不能被当成已存在。
		{"httpx", "httpx[http2]", false},
		{"httpx==0.28.1", "httpx[http2]", false},
		// 旧 extras ⊇ 新 extras 才算覆盖。
		{"httpx[http2]", "httpx[http2]", true},
		{"httpx[http2,socks]", "httpx[socks]", true},
		{"httpx[http2, socks]==0.28.1", "httpx[socks,http2]", true},
		{"httpx[socks]", "httpx[http2]", false},
		{"httpx[http2]", "httpx[http2,socks]", false},
		// extras 名按小写 + PEP 503 分隔符折叠比较。
		{"httpx[HTTP2]", "httpx[http2]", true},
		{"pkg[Foo_Bar]", "pkg[foo.bar]", true},
		// 环境标记里的方括号不算 extras。
		{"httpx; extra == 'x[y]'", "httpx[y]", false},
		{"httpx; python_version >= '3'", "httpx[http2]", false},
	}

	for _, tc := range cases {
		if got := PythonExtrasCovered(tc.existing, tc.requested); got != tc.want {
			t.Errorf("PythonExtrasCovered(%q, %q) = %v, want %v", tc.existing, tc.requested, got, tc.want)
		}
	}
}

// issue #166：原地升级时记录名 = 旧写法合并本次 extras，版本锁定、已有 extras、环境标记都不能丢。
func TestMergePythonSpecExtras(t *testing.T) {
	cases := []struct {
		existing  string
		requested string
		want      string
	}{
		{"httpx", "httpx[http2]", "httpx[http2]"},
		// 旧写法的版本锁定沿用。
		{"httpx==0.27.0", "httpx[http2]", "httpx[http2]==0.27.0"},
		// 旧 extras 保留在前，新 extras 追加在后。
		{"httpx[socks]", "httpx[http2]", "httpx[socks,http2]"},
		// 本次写法带版本约束时用本次的。
		{"httpx[socks]==0.27.0", "httpx[http2]>=0.28", "httpx[socks,http2]>=0.28"},
		{"httpx>=1,<2", "httpx[http2]", "httpx[http2]>=1,<2"},
		// 环境标记原样沿用，前导空白不动。
		{`httpx; python_version >= "3.8"`, "httpx[http2]", `httpx[http2]; python_version >= "3.8"`},
		// 基础名大小写按本次输入；extras 去重按小写 + PEP 503 折叠，旧的保持原拼写。
		{"HTTPX[Socks]", "httpx[socks,http2]", "httpx[Socks,http2]"},
		{"pkg[Foo_Bar]", "pkg[foo.bar,baz]", "pkg[Foo_Bar,baz]"},
		// 边界：旧写法方括号没闭合，残缺部分不带进新名字。
		{"httpx[socks", "httpx[http2]", "httpx[http2]"},
		// 边界：本次写法首尾空白、基础名与 [ 之间、extras 里的空白都要能拆开。
		{"httpx==0.27.0", "  httpx [ http2 , socks ]  ", "httpx[http2,socks]==0.27.0"},
		{"httpx", " httpx[http2] >= 0.28 ", "httpx[http2] >= 0.28"},
	}

	for _, tc := range cases {
		if got := MergePythonSpecExtras(tc.existing, tc.requested); got != tc.want {
			t.Errorf("MergePythonSpecExtras(%q, %q) = %q, want %q", tc.existing, tc.requested, got, tc.want)
		}
	}
}

func TestFindExistingPythonDependencyIgnoresCaseAndSeparators(t *testing.T) {
	testutil.SetupTestEnv(t)

	if err := database.DB.Create(&model.Dependency{
		Type:          model.DepTypePython,
		Name:          "requests",
		PythonVersion: "3.12",
		Status:        model.DepStatusInstalled,
	}).Error; err != nil {
		t.Fatalf("seed dependency: %v", err)
	}

	// 大小写不同的同一个包应被识别为已存在。
	if _, ok := FindExistingPythonDependency("Requests", "3.12", model.DepStatusInstalled); !ok {
		t.Fatal("expected Requests to match existing requests record")
	}
	// 带版本号的同一个包也应识别为已存在。
	if _, ok := FindExistingPythonDependency("requests==2.31.0", "3.12"); !ok {
		t.Fatal("expected requests==2.31.0 to match existing requests record")
	}
	// 不同 Python 版本不应命中。
	if _, ok := FindExistingPythonDependency("requests", "3.10"); ok {
		t.Fatal("expected no match for a different python version")
	}
	// 不同的包不应命中。
	if _, ok := FindExistingPythonDependency("flask", "3.12"); ok {
		t.Fatal("expected no match for a different package")
	}
}

func TestMergeDuplicatePythonDependenciesKeepsInstalledWinner(t *testing.T) {
	testutil.SetupTestEnv(t)

	now := time.Now()
	seed := []model.Dependency{
		{Type: model.DepTypePython, Name: "requests", PythonVersion: "3.12", Status: model.DepStatusFailed, UpdatedAt: now.Add(-2 * time.Hour)},
		{Type: model.DepTypePython, Name: "Requests", PythonVersion: "3.12", Status: model.DepStatusInstalled, UpdatedAt: now.Add(-1 * time.Hour)},
		{Type: model.DepTypePython, Name: "REQUESTS", PythonVersion: "3.12", Status: model.DepStatusInstalling, UpdatedAt: now},
		// 不同版本，应独立保留。
		{Type: model.DepTypePython, Name: "requests", PythonVersion: "3.10", Status: model.DepStatusInstalled, UpdatedAt: now},
		// 不同包，应保留。
		{Type: model.DepTypePython, Name: "flask", PythonVersion: "3.12", Status: model.DepStatusInstalled, UpdatedAt: now},
		// 非 Python，不参与合并。
		{Type: model.DepTypeNodeJS, Name: "Requests", PythonVersion: "", Status: model.DepStatusInstalled, UpdatedAt: now},
	}
	for i := range seed {
		if err := database.DB.Create(&seed[i]).Error; err != nil {
			t.Fatalf("seed dependency %d: %v", i, err)
		}
	}

	if err := MergeDuplicatePythonDependencies(); err != nil {
		t.Fatal(err)
	}

	// 3.12 的三条 requests 应合并为一条，保留 installed 的那条。
	var remaining []model.Dependency
	if err := database.DB.Where("type = ? AND python_version = ?", model.DepTypePython, "3.12").
		Where("name IN ?", []string{"requests", "Requests", "REQUESTS"}).Find(&remaining).Error; err != nil {
		t.Fatalf("query remaining: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining requests row for 3.12, got %d", len(remaining))
	}
	if remaining[0].Status != model.DepStatusInstalled {
		t.Fatalf("expected installed winner to survive, got status %q (name %q)", remaining[0].Status, remaining[0].Name)
	}

	// 其它分组应原样保留。
	var total int64
	database.DB.Model(&model.Dependency{}).Count(&total)
	// 合并后剩：3.12 requests(1) + 3.10 requests(1) + 3.12 flask(1) + nodejs Requests(1) = 4
	if total != 4 {
		t.Fatalf("expected 4 total dependency rows after merge, got %d", total)
	}

	// 幂等：再跑一次不应再删任何行。
	if err := MergeDuplicatePythonDependencies(); err != nil {
		t.Fatal(err)
	}
	database.DB.Model(&model.Dependency{}).Count(&total)
	if total != 4 {
		t.Fatalf("expected merge to be idempotent (still 4 rows), got %d", total)
	}
}
