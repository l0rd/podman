package shim

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/sirupsen/logrus"
	"go.podman.io/podman/v6/pkg/machine"
	machineDefine "go.podman.io/podman/v6/pkg/machine/define"
	"go.podman.io/podman/v6/pkg/machine/env"
	"go.podman.io/podman/v6/pkg/machine/vmconfigs"
)

const gvproxyHelperName = "gvproxy"

// HelperInfo describes a host helper process of a machine.
type HelperInfo struct {
	Machine      string
	VMType       string
	MachineState string
	Name         string
	// PID of the helper process, 0 if it isn't running
	PID     int
	Running bool
}

// HelpersOptions selects the machines and the helpers to act on.
type HelpersOptions struct {
	// Machines to act on, all the machines if empty
	Machines []string
	// Helpers to act on (e.g. "gvproxy"), all the helpers if empty
	Helpers []string
}

// HostHelpers returns the host helpers of a machine, in the order they have to
// be started.
func HostHelpers(mc *vmconfigs.MachineConfig, mp vmconfigs.VMProvider) ([]vmconfigs.HostHelper, error) {
	var helpers []vmconfigs.HostHelper
	if !mp.UseProviderNetworkSetup() {
		dirs, err := env.GetMachineDirs(mp.VMType())
		if err != nil {
			return nil, err
		}
		helpers = append(helpers, &gvproxyHelper{mc: mc, mp: mp, dirs: dirs})
	}
	if hp, ok := mp.(vmconfigs.HostHelpersProvider); ok {
		helpers = append(helpers, hp.HostHelpers(mc)...)
	}
	return helpers, nil
}

// ListHelpers returns the host helpers of the selected machines and their state.
func ListHelpers(providers []vmconfigs.VMProvider, opts HelpersOptions) ([]HelperInfo, error) {
	machines, err := selectMachines(providers, opts.Machines)
	if err != nil {
		return nil, err
	}
	var infos []HelperInfo
	for _, m := range machines {
		state, err := m.Provider.State(m.MachineConfig, false)
		if err != nil {
			return nil, fmt.Errorf("getting state of machine %q: %w", m.MachineConfig.Name, err)
		}
		helpers, err := selectHelpers(m, opts.Helpers)
		if err != nil {
			return nil, err
		}
		for _, h := range helpers {
			pid, err := h.PID()
			if err != nil {
				return nil, fmt.Errorf("getting %s PID of machine %q: %w", h.Name(), m.MachineConfig.Name, err)
			}
			infos = append(infos, HelperInfo{
				Machine:      m.MachineConfig.Name,
				VMType:       m.Provider.VMType().String(),
				MachineState: string(state),
				Name:         h.Name(),
				PID:          pid,
				Running:      pid != 0,
			})
		}
	}
	return infos, nil
}

// StartHelpers starts the helpers that aren't running for the selected
// machines that are running. Machines that aren't running are skipped,
// unless explicitly selected. It returns the helpers that have been started.
func StartHelpers(providers []vmconfigs.VMProvider, opts HelpersOptions) ([]HelperInfo, error) {
	machines, err := selectMachines(providers, opts.Machines)
	if err != nil {
		return nil, err
	}
	var (
		started []HelperInfo
		errs    []error
	)
	for _, m := range machines {
		s, err := startMachineHelpers(m, opts, len(opts.Machines) > 0)
		started = append(started, s...)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return started, errors.Join(errs...)
}

func startMachineHelpers(m knownMachineConfig, opts HelpersOptions, explicit bool) ([]HelperInfo, error) {
	mc := m.MachineConfig
	mc.Lock()
	defer mc.Unlock()
	if err := mc.Refresh(); err != nil {
		return nil, fmt.Errorf("reload config of machine %q: %w", mc.Name, err)
	}

	state, err := m.Provider.State(mc, false)
	if err != nil {
		return nil, fmt.Errorf("getting state of machine %q: %w", mc.Name, err)
	}
	if state != machineDefine.Running {
		if explicit {
			return nil, fmt.Errorf("machine %q is not running", mc.Name)
		}
		logrus.Debugf("Skipping helpers of machine %q: machine is %s", mc.Name, state)
		return nil, nil
	}

	helpers, err := selectHelpers(m, opts.Helpers)
	if err != nil {
		return nil, err
	}
	var started []HelperInfo
	for _, h := range helpers {
		pid, err := h.PID()
		if err != nil {
			return started, fmt.Errorf("getting %s PID of machine %q: %w", h.Name(), mc.Name, err)
		}
		if pid != 0 {
			logrus.Debugf("%s of machine %q is already running (PID %d)", h.Name(), mc.Name, pid)
			continue
		}
		// Helpers are started in order and may depend on the previous ones
		// (e.g. the 9p server on gvproxy) so stop at the first failure.
		if err := h.Start(); err != nil {
			return started, fmt.Errorf("starting %s of machine %q: %w", h.Name(), mc.Name, err)
		}
		pid, _ = h.PID()
		started = append(started, HelperInfo{
			Machine:      mc.Name,
			VMType:       m.Provider.VMType().String(),
			MachineState: string(state),
			Name:         h.Name(),
			PID:          pid,
			Running:      pid != 0,
		})
	}
	return started, nil
}

// StopHelpers stops the running helpers of the selected machines, whatever
// the machine state. It returns the helpers that have been stopped.
func StopHelpers(providers []vmconfigs.VMProvider, opts HelpersOptions) ([]HelperInfo, error) {
	machines, err := selectMachines(providers, opts.Machines)
	if err != nil {
		return nil, err
	}
	var (
		stopped []HelperInfo
		errs    []error
	)
	for _, m := range machines {
		s, err := stopMachineHelpers(m, opts)
		stopped = append(stopped, s...)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return stopped, errors.Join(errs...)
}

func stopMachineHelpers(m knownMachineConfig, opts HelpersOptions) ([]HelperInfo, error) {
	mc := m.MachineConfig
	mc.Lock()
	defer mc.Unlock()

	helpers, err := selectHelpers(m, opts.Helpers)
	if err != nil {
		return nil, err
	}
	var (
		stopped []HelperInfo
		errs    []error
	)
	// Stop in reverse start order
	for _, h := range slices.Backward(helpers) {
		pid, err := h.PID()
		if err != nil {
			errs = append(errs, fmt.Errorf("getting %s PID of machine %q: %w", h.Name(), mc.Name, err))
			continue
		}
		if pid == 0 {
			continue
		}
		if err := h.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("stopping %s of machine %q: %w", h.Name(), mc.Name, err))
			continue
		}
		stopped = append(stopped, HelperInfo{
			Machine: mc.Name,
			VMType:  m.Provider.VMType().String(),
			Name:    h.Name(),
			PID:     pid,
		})
	}
	return stopped, errors.Join(errs...)
}

// selectMachines returns the machines with the given names, or all the
// machines if names is empty, sorted by name.
func selectMachines(providers []vmconfigs.VMProvider, names []string) ([]knownMachineConfig, error) {
	mcs, err := getMCsOverProviders(providers)
	if err != nil {
		return nil, err
	}
	var machines []knownMachineConfig
	if len(names) == 0 {
		for _, m := range mcs {
			machines = append(machines, m)
		}
	} else {
		for _, name := range names {
			m, ok := mcs[name]
			if !ok {
				return nil, &machineDefine.ErrVMDoesNotExist{Name: name}
			}
			machines = append(machines, m)
		}
	}
	slices.SortFunc(machines, func(a, b knownMachineConfig) int {
		return strings.Compare(a.MachineConfig.Name, b.MachineConfig.Name)
	})
	return machines, nil
}

// selectHelpers returns the helpers of the machine with the given names, or
// all its helpers if names is empty.
func selectHelpers(m knownMachineConfig, names []string) ([]vmconfigs.HostHelper, error) {
	helpers, err := HostHelpers(m.MachineConfig, m.Provider)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return helpers, nil
	}
	return slices.DeleteFunc(helpers, func(h vmconfigs.HostHelper) bool {
		return !slices.Contains(names, h.Name())
	}), nil
}

// gvproxyHelper is the network and API forwarder of the providers that don't
// use their own network setup.
type gvproxyHelper struct {
	mc   *vmconfigs.MachineConfig
	mp   vmconfigs.VMProvider
	dirs *machineDefine.MachineDirs
}

func (h *gvproxyHelper) Name() string {
	return gvproxyHelperName
}

func (h *gvproxyHelper) pidFile() (*machineDefine.VMFile, error) {
	return h.dirs.RuntimeDir.AppendToNewVMFile("gvproxy.pid", nil)
}

func (h *gvproxyHelper) PID() (int, error) {
	pidFile, err := h.pidFile()
	if err != nil {
		return 0, err
	}
	pid, err := pidFile.ReadPIDFrom()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	if !machine.IsProcessAlive(pid) {
		return 0, nil
	}
	return pid, nil
}

// Start starts gvproxy for a running machine. The guest has to reconnect to
// the new gvproxy instance for its network to work again.
func (h *gvproxyHelper) Start() error {
	if err := cleanupStaleHostForwarder(h.mc, h.mp); err != nil {
		return err
	}
	if err := removeStaleAPISocket(h.mc, h.mp); err != nil {
		return err
	}
	hostSocks, _, _, err := setupMachineSockets(h.mc, h.dirs)
	if err != nil {
		return err
	}
	notifier, err := startHostForwarder(h.mc, h.mp, h.dirs, hostSocks)
	if err != nil {
		return err
	}
	defer notifier.Close()

	notifyCtx, notifyCancel := context.WithCancel(context.Background())
	defer notifyCancel()
	go notifier.Start(notifyCtx)

	readyCtx, readyCancel := context.WithTimeout(context.Background(), machine.GvProxyReadyTimeout)
	defer readyCancel()
	if err := notifier.WaitReady(readyCtx); err != nil {
		return fmt.Errorf("waiting for gvproxy readiness: %w", err)
	}
	return nil
}

func (h *gvproxyHelper) Stop() error {
	pidFile, err := h.pidFile()
	if err != nil {
		return err
	}
	return machine.CleanupGVProxy(*pidFile)
}
