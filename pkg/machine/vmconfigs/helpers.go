package vmconfigs

// HostHelper is a process running on the host that a running machine
// depends on, such as gvproxy, win-sshproxy or the 9p file server.
type HostHelper interface {
	// Name is the name of the helper (e.g. "gvproxy").
	Name() string
	// PID returns the PID of the helper process, or 0 if it isn't running.
	PID() (int, error)
	// Start starts the helper for a machine that is already running.
	Start() error
	// Stop stops the helper.
	Stop() error
}

// HostHelpersProvider is implemented by the providers that have
// provider-specific host helpers. The helpers common to all the providers
// (gvproxy) are handled by the shim.
type HostHelpersProvider interface {
	// HostHelpers returns the provider-specific helpers of the machine,
	// in the order they have to be started.
	HostHelpers(mc *MachineConfig) []HostHelper
}
