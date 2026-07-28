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

	originalInstalled := dependencyInstalledFunc
	originalBatch := dependencyReinstallBatchFunc
	originalRestartBatch := dependencyRestartReinstallBatchFunc
	defer func() {
		dependencyInstalledFunc = originalInstalled
		dependencyReinstallBatchFunc = originalBatch
		dependencyRestartReinstallBatchFunc = originalRestartBatch
	}()
	dependencyInstalledFunc = func(depType, name, pythonVersion string) bool { return false }
	dependencyReinstallBatchFunc = func(deps []model.Dependency) {}
	dependencyRestartReinstallBatchFunc = func(deps []model.Dependency) {}

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
}
