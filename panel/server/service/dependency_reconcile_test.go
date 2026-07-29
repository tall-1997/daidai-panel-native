package service

import (
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

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

	runner := dependencyReconcileRunner{
		installed:        func(depType, name, pythonVersion string) bool { return false },
		reinstall:        func(deps []model.Dependency) {},
		restartReinstall: func(deps []model.Dependency) {},
	}
	reconcileDependenciesAfterRestart(runner)

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

	reconcileDependenciesAfterRestart(runner)

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

	runner := dependencyReconcileRunner{
		installed:        func(depType, name, pythonVersion string) bool { return true },
		reinstall:        func(deps []model.Dependency) {},
		restartReinstall: func(deps []model.Dependency) {},
	}
	reconcileDependenciesAfterRestart(runner)

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
	runner := dependencyReconcileRunner{
		installed: func(depType, name, pythonVersion string) bool { return false },
		reinstall: func(deps []model.Dependency) {},
		restartReinstall: func(deps []model.Dependency) {
			scheduled = append(scheduled, append([]model.Dependency(nil), deps...))
		},
	}
	reconcileDependenciesAfterRestart(runner)
	reconcileDependenciesAfterRestart(runner)

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
