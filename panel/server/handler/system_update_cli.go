package handler

import "fmt"

type PanelUpdatePlanInfo struct {
	DeploymentType string
	ContainerName  string
	ImageName      string
	PullImageName  string
	Channel        string
	MirrorHost     string
	RegistryURL    string
	ReleaseVersion string
	AssetName      string
	InstallDir     string
	BinaryName     string
}

type PanelUpdateStatusInfo struct {
	Status         string
	Phase          string
	Message        string
	Error          string
	DeploymentType string
	ContainerName  string
	ImageName      string
	PullImageName  string
	MirrorHost     string
	RegistryURL    string
	ReleaseVersion string
	AssetName      string
	InstallDir     string
	BinaryName     string
}

func BuildPanelUpdatePlanInfo() (PanelUpdatePlanInfo, error) {
	plan, err := buildPanelUpdatePlan()
	if err != nil {
		return PanelUpdatePlanInfo{}, err
	}

	return PanelUpdatePlanInfo{
		DeploymentType: plan.DeploymentType,
		ContainerName:  plan.ContainerName,
		ImageName:      plan.ImageName,
		PullImageName:  plan.PullImageName,
		Channel:        plan.Channel,
		MirrorHost:     plan.MirrorHost,
		RegistryURL:    plan.RegistryURL,
		ReleaseVersion: plan.ReleaseVersion,
		AssetName:      plan.AssetName,
		InstallDir:     plan.InstallDir,
		BinaryName:     plan.BinaryName,
	}, nil
}

func ExecutePanelUpdateForCLI() (PanelUpdateStatusInfo, error) {
	plan, err := buildPanelUpdatePlan()
	if err != nil {
		return PanelUpdateStatusInfo{}, err
	}

	executePanelUpdate(plan)

	snapshot := panelUpdater.snapshotCopy()
	status := PanelUpdateStatusInfo{
		Status:         snapshot.Status,
		Phase:          snapshot.Phase,
		Message:        snapshot.Message,
		Error:          snapshot.Error,
		DeploymentType: snapshot.DeploymentType,
		ContainerName:  snapshot.ContainerName,
		ImageName:      snapshot.ImageName,
		PullImageName:  snapshot.PullImageName,
		MirrorHost:     snapshot.MirrorHost,
		RegistryURL:    snapshot.RegistryURL,
		ReleaseVersion: snapshot.ReleaseVersion,
		AssetName:      snapshot.AssetName,
		InstallDir:     snapshot.InstallDir,
		BinaryName:     snapshot.BinaryName,
	}

	if snapshot.Status == "failed" {
		if snapshot.Error != "" {
			return status, fmt.Errorf("%s", snapshot.Error)
		}
		return status, fmt.Errorf("%s", snapshot.Message)
	}

	return status, nil
}
