package service

import (
	"os"
	"path/filepath"
	"testing"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// stubDependencyReconcile 把启动校验的三个注入点替换成测试替身，返回恢复函数。
// v3.4.0 的注入点是包级函数变量（dependencyInstalledFunc / dependencyReinstallBatchFunc /
// dependencyRestartReinstallBatchFunc），不再是 dependency reconcile runner 结构体。
func stubDependencyReconcile(
	t *testing.T,
	installed func(depType, name, pythonVersion string) bool,
	reinstall func(deps []model.Dependency),
	restartReinstall func(deps []model.Dependency),
) {
	t.Helper()
	originalInstalled := dependencyInstalledFunc
	originalReinstall := dependencyReinstallBatchFunc
	originalRestartReinstall := dependencyRestartReinstallBatchFunc
	originalSnapshot := pythonInstalledPackagesFunc
	dependencyInstalledFunc = installed
	dependencyReinstallBatchFunc = reinstall
	dependencyRestartReinstallBatchFunc = restartReinstall
	// Python 走逐条校验，避免真起 pip list。
	pythonInstalledPackagesFunc = func(string) (map[string]bool, error) { return nil, nil }
	t.Cleanup(func() {
		dependencyInstalledFunc = originalInstalled
		dependencyReinstallBatchFunc = originalReinstall
		dependencyRestartReinstallBatchFunc = originalRestartReinstall
		pythonInstalledPackagesFunc = originalSnapshot
	})
}

func TestReconcileDependenciesAfterRestartKeepsPrivateDependenciesInstalled(t *testing.T) {
	testutil.SetupTestEnv(t)
	pythonVersion := "3.11"
	pythonDistInfo := filepath.Join(ManagedPythonSitePackagesDir(pythonVersion), "offline_fixture-1.0.0.dist-info")
	nodePackage := filepath.Join(config.C.Data.Dir, "deps", "nodejs", "node_modules", "offline-fixture")
	for _, dir := range []string{pythonDistInfo, nodePackage} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create private dependency fixture: %v", err)
		}
	}
	dependencies := []model.Dependency{
		{Type: model.DepTypePython, Name: "offline-fixture", PythonVersion: pythonVersion, Status: model.DepStatusInstalled},
		{Type: model.DepTypeNodeJS, Name: "offline-fixture", Status: model.DepStatusInstalled},
	}
	if err := database.DB.Create(&dependencies).Error; err != nil {
		t.Fatalf("create dependencies: %v", err)
	}

	reinstallCalls := 0
	stubDependencyReconcile(t,
		func(depType, name, pythonVersion string) bool { return true },
		func(deps []model.Dependency) { reinstallCalls += len(deps) },
		func(deps []model.Dependency) { reinstallCalls += len(deps) },
	)
	ReconcileDependenciesAfterRestart()

	if reinstallCalls != 0 {
		t.Fatalf("expected private dependencies to survive restart, got %d reinstall calls", reinstallCalls)
	}
	var stored []model.Dependency
	if err := database.DB.Order("id ASC").Find(&stored).Error; err != nil {
		t.Fatalf("load dependencies: %v", err)
	}
	for _, dep := range stored {
		if dep.Status != model.DepStatusInstalled {
			t.Fatalf("expected installed status after restart, got %+v", dep)
		}
	}
}

func TestReconcileDependenciesAfterRestartRecoversInterruptedOperation(t *testing.T) {
	testutil.SetupTestEnv(t)

	dep := model.Dependency{Type: model.DepTypeNodeJS, Name: "left-pad", Status: model.DepStatusQueued, OperationID: "dep_recover"}
	if err := database.DB.Create(&dep).Error; err != nil {
		t.Fatalf("create dependency: %v", err)
	}
	op := model.Operation{ID: "dep_recover", Kind: model.OperationKindDependency, State: model.OperationStateRunning, Phase: "running"}
	if err := database.DB.Create(&op).Error; err != nil {
		t.Fatalf("create operation: %v", err)
	}

	stubDependencyReconcile(t,
		func(depType, name, pythonVersion string) bool { return false },
		func(deps []model.Dependency) {},
		func(deps []model.Dependency) {},
	)
	ReconcileDependenciesAfterRestart()

	var recovered model.Operation
	if err := database.DB.First(&recovered, "id = ?", "dep_recover").Error; err != nil {
		t.Fatalf("load operation: %v", err)
	}
	if recovered.State != model.OperationStateUnknown || recovered.ErrorCode == "" {
		t.Fatalf("expected recovered unknown operation, got %+v", recovered)
	}

	var reloaded model.Dependency
	if err := database.DB.First(&reloaded, dep.ID).Error; err != nil {
		t.Fatalf("load dependency: %v", err)
	}
	if reloaded.Status != model.DepStatusFailed {
		t.Fatalf("expected queued dependency reset to failed, got %+v", reloaded)
	}

	ReconcileDependenciesAfterRestart()

	var reconciledAgain model.Dependency
	if err := database.DB.First(&reconciledAgain, dep.ID).Error; err != nil {
		t.Fatalf("reload dependency after second reconcile: %v", err)
	}
	if reconciledAgain.Status != model.DepStatusFailed || reconciledAgain.Log != reloaded.Log {
		t.Fatalf("expected repeated reconcile to preserve recovered dependency, got %+v", reconciledAgain)
	}
}

func TestReconcileDependenciesAfterRestartFinishesDetectedInterruptedOperation(t *testing.T) {
	testutil.SetupTestEnv(t)

	dep := model.Dependency{Type: model.DepTypeLinux, Name: "curl", Status: model.DepStatusInstalling, OperationID: "dep_installed"}
	if err := database.DB.Create(&dep).Error; err != nil {
		t.Fatalf("create dependency: %v", err)
	}
	op := model.Operation{ID: dep.OperationID, Kind: model.OperationKindDependency, State: model.OperationStateRunning, Phase: "running"}
	if err := database.DB.Create(&op).Error; err != nil {
		t.Fatalf("create operation: %v", err)
	}

	stubDependencyReconcile(t,
		func(depType, name, pythonVersion string) bool { return true },
		func(deps []model.Dependency) {},
		func(deps []model.Dependency) {},
	)
	ReconcileDependenciesAfterRestart()

	var recovered model.Operation
	if err := database.DB.First(&recovered, "id = ?", op.ID).Error; err != nil {
		t.Fatalf("load operation: %v", err)
	}
	if recovered.State != model.OperationStateSuccess || recovered.ErrorCode != "" {
		t.Fatalf("expected detected dependency operation to finish successfully, got %+v", recovered)
	}
}

func TestReconcileDependenciesAfterRestartIsReentrantForRestartReinstall(t *testing.T) {
	testutil.SetupTestEnv(t)

	dependencies := []model.Dependency{
		{Type: model.DepTypeLinux, Name: "curl", Status: model.DepStatusInstalled},
		{Type: model.DepTypeNodeJS, Name: "left-pad", Status: model.DepStatusInstalled},
		{Type: model.DepTypePython, Name: "requests", Status: model.DepStatusInstalled},
	}
	if err := database.DB.Create(&dependencies).Error; err != nil {
		t.Fatalf("create dependencies: %v", err)
	}

	var scheduled [][]model.Dependency
	stubDependencyReconcile(t,
		func(depType, name, pythonVersion string) bool { return false },
		func(deps []model.Dependency) {},
		func(deps []model.Dependency) {
			scheduled = append(scheduled, append([]model.Dependency(nil), deps...))
		},
	)
	ReconcileDependenciesAfterRestart()
	ReconcileDependenciesAfterRestart()

	var recovered []model.Dependency
	if err := database.DB.Order("id ASC").Find(&recovered).Error; err != nil {
		t.Fatalf("load dependencies: %v", err)
	}
	for _, dep := range recovered {
		if dep.Status != model.DepStatusInstalling {
			t.Fatalf("expected repeated reconcile to preserve installing status, got %+v", dep)
		}
	}
	if len(scheduled) != 2 || len(scheduled[0]) != len(dependencies) || len(scheduled[1]) != len(dependencies) {
		t.Fatalf("expected each recovery pass to schedule all dependencies, got %+v", scheduled)
	}
}