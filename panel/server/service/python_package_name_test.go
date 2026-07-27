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
		"requests":                    "requests",
		"Requests":                    "requests",
		"  REQUESTS  ":                "requests",
		"Flask_SQLAlchemy":            "flask-sqlalchemy",
		"flask-sqlalchemy":            "flask-sqlalchemy",
		"zope.interface":              "zope-interface",
		"requests==2.31.0":            "requests",
		"requests>=2.0":               "requests",
		"requests~=2.31.0":            "requests",
		"requests!=2.0":               "requests",
		"requests<3":                  "requests",
		"requests[security]":          "requests",
		"zope.interface[test]":        "zope-interface",
		"requests; python_version<'3'": "requests",
		"requests == 2.31.0":          "requests",
		"":                            "",
		"   ":                         "",
	}

	for input, expected := range cases {
		if got := CanonicalizePythonPackageName(input); got != expected {
			t.Errorf("CanonicalizePythonPackageName(%q) = %q, want %q", input, got, expected)
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

	MergeDuplicatePythonDependencies()

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
	MergeDuplicatePythonDependencies()
	database.DB.Model(&model.Dependency{}).Count(&total)
	if total != 4 {
		t.Fatalf("expected merge to be idempotent (still 4 rows), got %d", total)
	}
}
