//go:build windows

package hyperv

import (
	"errors"
	"io/fs"
	"os"

	"go.podman.io/podman/v6/pkg/machine"
	"go.podman.io/podman/v6/pkg/machine/vmconfigs"
)

const server9pHelperName = "server9p"

// HostHelpers returns the 9p file server of the machine, if it has mounts.
// gvproxy is handled by the shim.
func (h HyperVStubber) HostHelpers(mc *vmconfigs.MachineConfig) []vmconfigs.HostHelper {
	if len(mc.Mounts) == 0 {
		return nil
	}
	return []vmconfigs.HostHelper{&server9pHelper{mc: mc}}
}

type server9pHelper struct {
	mc *vmconfigs.MachineConfig
}

func (h *server9pHelper) Name() string {
	return server9pHelperName
}

func (h *server9pHelper) PID() (int, error) {
	pidFile, err := server9pPIDFile(h.mc)
	if err != nil {
		return 0, err
	}
	pid, err := pidFile.ReadPIDFrom()
	if err != nil {
		// Machines started before the PID file was introduced don't have it
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

// Start starts the 9p server and mounts the shares again in the guest, as the
// mounts served by the previous server are not usable anymore. It requires
// gvproxy to be running.
func (h *server9pHelper) Start() error {
	if err := startServer9p(h.mc); err != nil {
		return err
	}
	return startShares(h.mc)
}

func (h *server9pHelper) Stop() error {
	pid, err := h.PID()
	if err != nil {
		return err
	}
	if pid != 0 {
		p, err := os.FindProcess(pid)
		if err == nil {
			if err := p.Kill(); err != nil {
				return err
			}
			_, _ = p.Wait()
		}
	}
	pidFile, err := server9pPIDFile(h.mc)
	if err != nil {
		return err
	}
	return pidFile.Delete()
}
