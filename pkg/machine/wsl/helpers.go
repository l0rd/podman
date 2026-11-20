//go:build windows

package wsl

import (
	"fmt"

	"go.podman.io/podman/v6/pkg/machine"
	"go.podman.io/podman/v6/pkg/machine/vmconfigs"
)

const winSSHProxyHelperName = "win-sshproxy"

// HostHelpers returns the API forwarder of the machine (win-sshproxy).
func (w WSLStubber) HostHelpers(mc *vmconfigs.MachineConfig) []vmconfigs.HostHelper {
	return []vmconfigs.HostHelper{&winSSHProxyHelper{mc: mc}}
}

type winSSHProxyHelper struct {
	mc *vmconfigs.MachineConfig
}

func (h *winSSHProxyHelper) Name() string {
	return winSSHProxyHelperName
}

func (h *winSSHProxyHelper) PID() (int, error) {
	return machine.WinProxyPID(h.mc.Name, vmtype)
}

func (h *winSSHProxyHelper) Start() error {
	// A killed win-sshproxy (e.g. by the installer Restart Manager) leaves
	// its PID/TID file and its API socket behind.
	if err := machine.CleanupStaleWinProxy(h.mc.Name, vmtype); err != nil {
		return err
	}
	socket, err := h.mc.APISocket()
	if err != nil {
		return err
	}
	if err := socket.Delete(); err != nil {
		return fmt.Errorf("removing stale API socket %q: %w", socket.GetPath(), err)
	}

	return machine.StartWinProxy(machine.WinProxyOpts{
		Name:           h.mc.Name,
		IdentityPath:   h.mc.SSH.IdentityPath,
		Port:           h.mc.SSH.Port,
		RemoteUsername: h.mc.SSH.RemoteUsername,
		Rootful:        h.mc.HostUser.Rootful,
		VMType:         vmtype,
		Socket:         socket,
	})
}

func (h *winSSHProxyHelper) Stop() error {
	return machine.StopWinProxy(h.mc.Name, vmtype)
}
