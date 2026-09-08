//go:build windows

package wslc

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/google/uuid"
	netTypes "go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/config"
	"go.podman.io/podman/v6/libpod/define"
	"go.podman.io/podman/v6/pkg/domain/entities"
	"go.podman.io/podman/v6/pkg/domain/entities/reports"
	"go.podman.io/podman/v6/pkg/specgen"
)

var errNotSupported = fmt.Errorf("not supported by wslc engine")

func specToRunArgs(s *specgen.SpecGenerator, detach bool) []string {
	args := []string{"run"}
	if detach {
		args = append(args, "-d")
	}
	if s.Remove != nil && *s.Remove {
		args = append(args, "--rm")
	}
	if s.Name != "" {
		args = append(args, "--name", s.Name)
	}
	if s.Terminal != nil && *s.Terminal {
		args = append(args, "-it")
	}
	for _, pm := range s.PortMappings {
		args = append(args, "-p", fmt.Sprintf("%d:%d", pm.HostPort, pm.ContainerPort))
	}
	args = append(args, s.Image)
	args = append(args, s.Command...)
	return args
}

func (e *ContainerEngine) ContainerCreate(ctx context.Context, s *specgen.SpecGenerator) (*entities.ContainerCreateReport, error) {
	id := uuid.New().String()
	name := s.Name
	if name == "" {
		name = id[:12]
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.pending[id] = &pendingContainer{
		ID:   id,
		Name: name,
		Spec: s,
	}

	return &entities.ContainerCreateReport{Id: id}, nil
}

func (e *ContainerEngine) ContainerStart(ctx context.Context, namesOrIds []string, options entities.ContainerStartOptions) ([]*entities.ContainerStartReport, error) {
	var startReports []*entities.ContainerStartReport
	for _, nameOrID := range namesOrIds {
		report := &entities.ContainerStartReport{Id: nameOrID, RawInput: nameOrID}

		e.mu.Lock()
		pc, found := e.pending[nameOrID]
		if !found {
			for _, p := range e.pending {
				if p.Name == nameOrID {
					pc = p
					found = true
					break
				}
			}
		}
		e.mu.Unlock()

		if found {
			args := specToRunArgs(pc.Spec, true)
			out, err := runWslc(args...)
			if err != nil {
				report.Err = err
			} else {
				realID := strings.TrimSpace(string(out))
				report.Id = realID
				e.mu.Lock()
				delete(e.pending, pc.ID)
				e.mu.Unlock()
			}
		} else {
			report.Err = fmt.Errorf("container %s not found in pending containers", nameOrID)
		}

		startReports = append(startReports, report)
	}
	return startReports, nil
}

func (e *ContainerEngine) ContainerRun(ctx context.Context, opts entities.ContainerRunOptions) (*entities.ContainerRunReport, error) {
	args := specToRunArgs(opts.Spec, opts.Detach)

	if !opts.Detach {
		cmd := execCommand(wslcBinary, args...)
		cmd.Stdin = opts.InputStream
		cmd.Stdout = opts.OutputStream
		cmd.Stderr = opts.ErrorStream
		err := cmd.Run()
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				return nil, err
			}
		}
		return &entities.ContainerRunReport{ExitCode: exitCode}, nil
	}

	out, err := runWslc(args...)
	if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(string(out))
	return &entities.ContainerRunReport{Id: id}, nil
}

func (e *ContainerEngine) ContainerStop(ctx context.Context, namesOrIds []string, options entities.StopOptions) ([]*entities.StopReport, error) {
	var stopReports []*entities.StopReport
	for _, nameOrID := range namesOrIds {
		report := &entities.StopReport{Id: nameOrID, RawInput: nameOrID}
		_, err := runWslc("container", "stop", nameOrID)
		if err != nil {
			report.Err = err
		}
		stopReports = append(stopReports, report)
	}
	return stopReports, nil
}

func (e *ContainerEngine) ContainerRm(ctx context.Context, namesOrIds []string, options entities.RmOptions) ([]*reports.RmReport, error) {
	var rmReports []*reports.RmReport
	for _, nameOrID := range namesOrIds {
		report := &reports.RmReport{Id: nameOrID, RawInput: nameOrID}
		_, err := runWslc("container", "stop", nameOrID)
		if err != nil {
			report.Err = err
		}
		rmReports = append(rmReports, report)
	}
	return rmReports, nil
}

func (e *ContainerEngine) ContainerList(ctx context.Context, options entities.ContainerListOptions) ([]entities.ListContainer, error) {
	args := []string{"container", "list"}
	if options.All {
		args = append(args, "--all")
	}
	out, err := runWslc(args...)
	if err != nil {
		return nil, err
	}
	return parseContainerList(out)
}

func (e *ContainerEngine) ContainerInspect(ctx context.Context, namesOrIds []string, options entities.InspectOptions) ([]*entities.ContainerInspectReport, []error, error) {
	var inspectReports []*entities.ContainerInspectReport
	var errs []error
	for _, nameOrID := range namesOrIds {
		var raw map[string]interface{}
		err := runWslcJSON(&raw, "container", "inspect", nameOrID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		report, convertErr := convertInspect(raw)
		if convertErr != nil {
			errs = append(errs, convertErr)
			continue
		}
		inspectReports = append(inspectReports, report)
	}
	return inspectReports, errs, nil
}

func (e *ContainerEngine) ContainerExists(ctx context.Context, nameOrID string, options entities.ContainerExistsOptions) (*entities.BoolReport, error) {
	_, err := runWslc("container", "inspect", nameOrID)
	return &entities.BoolReport{Value: err == nil}, nil
}

func (e *ContainerEngine) ContainerPrune(ctx context.Context, options entities.ContainerPruneOptions) ([]*reports.PruneReport, error) {
	_, err := runWslc("container", "prune")
	if err != nil {
		return nil, err
	}
	return []*reports.PruneReport{}, nil
}

func (e *ContainerEngine) Version(ctx context.Context) (*entities.SystemVersionReport, error) {
	out, err := runWslc("version")
	if err != nil {
		return nil, err
	}
	version := strings.TrimSpace(string(out))
	return &entities.SystemVersionReport{
		Client: &define.Version{
			Version: version,
		},
	}, nil
}

func (e *ContainerEngine) Shutdown(ctx context.Context) {}

// --- Stubs for unimplemented methods ---

func (e *ContainerEngine) AutoUpdate(ctx context.Context, options entities.AutoUpdateOptions) ([]*entities.AutoUpdateReport, []error) {
	return nil, []error{errNotSupported}
}

func (e *ContainerEngine) Config(ctx context.Context) (*config.Config, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) ContainerAttach(ctx context.Context, nameOrID string, options entities.AttachOptions) error {
	return errNotSupported
}

func (e *ContainerEngine) ContainerCheckpoint(ctx context.Context, namesOrIds []string, options entities.CheckpointOptions) ([]*entities.CheckpointReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) ContainerCleanup(ctx context.Context, namesOrIds []string, options entities.ContainerCleanupOptions) ([]*entities.ContainerCleanupReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) ContainerClone(ctx context.Context, ctrClone entities.ContainerCloneOptions) (*entities.ContainerCreateReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) ContainerCommit(ctx context.Context, nameOrID string, options entities.CommitOptions) (*entities.CommitReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) ContainerCopyFromArchive(ctx context.Context, nameOrID, path string, reader io.Reader, options entities.CopyOptions) (entities.ContainerCopyFunc, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) ContainerCopyToArchive(ctx context.Context, nameOrID string, path string, writer io.Writer) (entities.ContainerCopyFunc, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) ContainerExec(ctx context.Context, nameOrID string, options entities.ExecOptions, streams define.AttachStreams) (int, error) {
	return -1, errNotSupported
}

func (e *ContainerEngine) ContainerExecNoSession(ctx context.Context, nameOrID string, options entities.ExecOptions, streams define.AttachStreams) (int, error) {
	return -1, errNotSupported
}

func (e *ContainerEngine) ContainerExecDetached(ctx context.Context, nameOrID string, options entities.ExecOptions) (string, error) {
	return "", errNotSupported
}

func (e *ContainerEngine) ContainerExport(ctx context.Context, nameOrID string, options entities.ContainerExportOptions) error {
	return errNotSupported
}

func (e *ContainerEngine) ContainerInit(ctx context.Context, namesOrIds []string, options entities.ContainerInitOptions) ([]*entities.ContainerInitReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) ContainerKill(ctx context.Context, namesOrIds []string, options entities.KillOptions) ([]*entities.KillReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) ContainerListExternal(ctx context.Context) ([]entities.ListContainer, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) ContainerLogs(ctx context.Context, containers []string, options entities.ContainerLogsOptions) error {
	return errNotSupported
}

func (e *ContainerEngine) ContainerMount(ctx context.Context, nameOrIDs []string, options entities.ContainerMountOptions) ([]*entities.ContainerMountReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) ContainerPause(ctx context.Context, namesOrIds []string, options entities.PauseUnPauseOptions) ([]*entities.PauseUnpauseReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) ContainerPort(ctx context.Context, nameOrID string, options entities.ContainerPortOptions) ([]*entities.ContainerPortReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) ContainerRename(ctx context.Context, nameOrID string, options entities.ContainerRenameOptions) error {
	return errNotSupported
}

func (e *ContainerEngine) ContainerRestart(ctx context.Context, namesOrIds []string, options entities.RestartOptions) ([]*entities.RestartReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) ContainerRestore(ctx context.Context, namesOrIds []string, options entities.RestoreOptions) ([]*entities.RestoreReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) ContainerRunlabel(ctx context.Context, label string, image string, args []string, opts entities.ContainerRunlabelOptions) error {
	return errNotSupported
}

func (e *ContainerEngine) ContainerStat(ctx context.Context, nameOrDir string, path string) (*entities.ContainerStatReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) ContainerStats(ctx context.Context, namesOrIds []string, options entities.ContainerStatsOptions) (chan entities.ContainerStatsReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) ContainerStopService(ctx context.Context, namesOrIds []string, options entities.StopOptions) ([]*entities.StopReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) ContainerTop(ctx context.Context, options entities.TopOptions) (*entities.StringSliceReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) ContainerUnmount(ctx context.Context, nameOrIDs []string, options entities.ContainerUnmountOptions) ([]*entities.ContainerUnmountReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) ContainerUnpause(ctx context.Context, namesOrIds []string, options entities.PauseUnPauseOptions) ([]*entities.PauseUnpauseReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) ContainerUpdate(ctx context.Context, options *entities.ContainerUpdateOptions) (string, error) {
	return "", errNotSupported
}

func (e *ContainerEngine) ContainerWait(ctx context.Context, namesOrIds []string, options entities.WaitOptions) ([]entities.WaitReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) Diff(ctx context.Context, namesOrIds []string, options entities.DiffOptions) (*entities.DiffReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) Events(ctx context.Context, opts entities.EventsOptions) error {
	return errNotSupported
}

func (e *ContainerEngine) GenerateSpec(ctx context.Context, opts *entities.GenerateSpecOptions) (*entities.GenerateSpecReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) GenerateSystemd(ctx context.Context, nameOrID string, opts entities.GenerateSystemdOptions) (*entities.GenerateSystemdReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) GenerateKube(ctx context.Context, nameOrIDs []string, opts entities.GenerateKubeOptions) (*entities.GenerateKubeReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) SystemPrune(ctx context.Context, options entities.SystemPruneOptions) (*entities.SystemPruneReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) HealthCheckRun(ctx context.Context, nameOrID string, options entities.HealthCheckOptions) (*define.HealthCheckResults, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) Info(ctx context.Context) (*define.Info, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) KubeApply(ctx context.Context, body io.Reader, opts entities.ApplyOptions) error {
	return errNotSupported
}

func (e *ContainerEngine) Locks(ctx context.Context) (*entities.LocksReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) Migrate(ctx context.Context, options entities.SystemMigrateOptions) error {
	return errNotSupported
}

func (e *ContainerEngine) NetworkConnect(ctx context.Context, networkname string, options entities.NetworkConnectOptions) error {
	return errNotSupported
}

func (e *ContainerEngine) NetworkCreate(ctx context.Context, network netTypes.Network, createOptions *netTypes.NetworkCreateOptions) (*netTypes.Network, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) NetworkUpdate(ctx context.Context, networkname string, options entities.NetworkUpdateOptions) error {
	return errNotSupported
}

func (e *ContainerEngine) NetworkDisconnect(ctx context.Context, networkname string, options entities.NetworkDisconnectOptions) error {
	return errNotSupported
}

func (e *ContainerEngine) NetworkExists(ctx context.Context, networkname string) (*entities.BoolReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) NetworkInspect(ctx context.Context, namesOrIds []string, options entities.InspectOptions) ([]entities.NetworkInspectReport, []error, error) {
	return nil, nil, errNotSupported
}

func (e *ContainerEngine) NetworkList(ctx context.Context, options entities.NetworkListOptions) ([]netTypes.Network, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) NetworkPrune(ctx context.Context, options entities.NetworkPruneOptions) ([]*entities.NetworkPruneReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) NetworkReload(ctx context.Context, names []string, options entities.NetworkReloadOptions) ([]*entities.NetworkReloadReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) NetworkRm(ctx context.Context, namesOrIds []string, options entities.NetworkRmOptions) ([]*entities.NetworkRmReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) PlayKube(ctx context.Context, body io.Reader, opts entities.PlayKubeOptions) (*entities.PlayKubeReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) PlayKubeDown(ctx context.Context, body io.Reader, opts entities.PlayKubeDownOptions) (*entities.PlayKubeReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) PodCreate(ctx context.Context, specg entities.PodSpec) (*entities.PodCreateReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) PodClone(ctx context.Context, podClone entities.PodCloneOptions) (*entities.PodCloneReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) PodExists(ctx context.Context, nameOrID string) (*entities.BoolReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) PodInspect(ctx context.Context, namesOrID []string, options entities.InspectOptions) ([]*entities.PodInspectReport, []error, error) {
	return nil, nil, errNotSupported
}

func (e *ContainerEngine) PodKill(ctx context.Context, namesOrIds []string, options entities.PodKillOptions) ([]*entities.PodKillReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) PodLogs(ctx context.Context, pod string, options entities.PodLogsOptions) error {
	return errNotSupported
}

func (e *ContainerEngine) PodPause(ctx context.Context, namesOrIds []string, options entities.PodPauseOptions) ([]*entities.PodPauseReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) PodPrune(ctx context.Context, options entities.PodPruneOptions) ([]*entities.PodPruneReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) PodPs(ctx context.Context, options entities.PodPSOptions) ([]*entities.ListPodsReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) PodRestart(ctx context.Context, namesOrIds []string, options entities.PodRestartOptions) ([]*entities.PodRestartReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) PodRm(ctx context.Context, namesOrIds []string, options entities.PodRmOptions) ([]*entities.PodRmReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) PodStart(ctx context.Context, namesOrIds []string, options entities.PodStartOptions) ([]*entities.PodStartReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) PodStats(ctx context.Context, namesOrIds []string, options entities.PodStatsOptions) ([]*entities.PodStatsReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) PodStop(ctx context.Context, namesOrIds []string, options entities.PodStopOptions) ([]*entities.PodStopReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) PodTop(ctx context.Context, options entities.PodTopOptions) (*entities.StringSliceReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) PodUnpause(ctx context.Context, namesOrIds []string, options entities.PodunpauseOptions) ([]*entities.PodUnpauseReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) QuadletExists(ctx context.Context, name string) (*entities.BoolReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) QuadletInstall(ctx context.Context, pathsOrURLs []string, options entities.QuadletInstallOptions) (*entities.QuadletInstallReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) QuadletList(ctx context.Context, options entities.QuadletListOptions) ([]*entities.ListQuadlet, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) QuadletPrint(ctx context.Context, quadlet string) (string, error) {
	return "", errNotSupported
}

func (e *ContainerEngine) QuadletRemove(ctx context.Context, quadlets []string, options entities.QuadletRemoveOptions) (*entities.QuadletRemoveReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) Renumber(ctx context.Context) error {
	return errNotSupported
}

func (e *ContainerEngine) Reset(ctx context.Context) error {
	return errNotSupported
}

func (e *ContainerEngine) SetupRootless(ctx context.Context, noMoveProcess bool, cgroupMode string) error {
	return nil
}

func (e *ContainerEngine) SecretCreate(ctx context.Context, name string, reader io.Reader, options entities.SecretCreateOptions) (*entities.SecretCreateReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) SecretInspect(ctx context.Context, nameOrIDs []string, options entities.SecretInspectOptions) ([]*entities.SecretInfoReport, []error, error) {
	return nil, nil, errNotSupported
}

func (e *ContainerEngine) SecretList(ctx context.Context, opts entities.SecretListRequest) ([]*entities.SecretInfoReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) SecretRm(ctx context.Context, nameOrID []string, opts entities.SecretRmOptions) ([]*entities.SecretRmReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) SecretExists(ctx context.Context, nameOrID string) (*entities.BoolReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) SystemDf(ctx context.Context, options entities.SystemDfOptions) (*entities.SystemDfReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) SystemCheck(ctx context.Context, options entities.SystemCheckOptions) (*entities.SystemCheckReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) Unshare(ctx context.Context, args []string, options entities.SystemUnshareOptions) error {
	return errNotSupported
}

func (e *ContainerEngine) VolumeCreate(ctx context.Context, opts entities.VolumeCreateOptions) (*entities.IDOrNameResponse, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) VolumeExists(ctx context.Context, namesOrID string) (*entities.BoolReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) VolumeMounted(ctx context.Context, namesOrID string) (*entities.BoolReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) VolumeInspect(ctx context.Context, namesOrIds []string, opts entities.InspectOptions) ([]*entities.VolumeInspectReport, []error, error) {
	return nil, nil, errNotSupported
}

func (e *ContainerEngine) VolumeList(ctx context.Context, opts entities.VolumeListOptions) ([]*entities.VolumeListReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) VolumeMount(ctx context.Context, namesOrIds []string) ([]*entities.VolumeMountReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) VolumePrune(ctx context.Context, options entities.VolumePruneOptions) ([]*reports.PruneReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) VolumeRename(ctx context.Context, nameOrID string, options entities.VolumeRenameOptions) error {
	return errNotSupported
}

func (e *ContainerEngine) VolumeRm(ctx context.Context, namesOrIds []string, opts entities.VolumeRmOptions) ([]*entities.VolumeRmReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) VolumeUnmount(ctx context.Context, namesOrIds []string) ([]*entities.VolumeUnmountReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) VolumeReload(ctx context.Context) (*entities.VolumeReloadReport, error) {
	return nil, errNotSupported
}

func (e *ContainerEngine) VolumeExport(ctx context.Context, nameOrID string, options entities.VolumeExportOptions) error {
	return errNotSupported
}

func (e *ContainerEngine) VolumeImport(ctx context.Context, nameOrID string, options entities.VolumeImportOptions) error {
	return errNotSupported
}
