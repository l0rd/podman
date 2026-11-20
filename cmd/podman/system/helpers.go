//go:build amd64 || arm64

package system

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"go.podman.io/common/pkg/completion"
	"go.podman.io/common/pkg/report"
	"go.podman.io/podman/v6/cmd/podman/common"
	"go.podman.io/podman/v6/cmd/podman/registry"
	"go.podman.io/podman/v6/cmd/podman/validate"
	provider2 "go.podman.io/podman/v6/pkg/machine/provider"
	"go.podman.io/podman/v6/pkg/machine/shim"
)

var (
	helpersCmd = &cobra.Command{
		Use:   "helpers",
		Short: "Manage the host helper processes of podman machines",
		Long: `Manage the processes that run on the host to support podman machines,
such as gvproxy, win-sshproxy and the 9p file server.`,
		// Like the machine commands, don't connect to a podman service
		PersistentPreRunE: validate.NoOp,
		RunE:              validate.SubCommandExists,
	}

	helpersLsCmd = &cobra.Command{
		Use:               "list [options] [MACHINE...]",
		Aliases:           []string{"ls"},
		Short:             "List the host helpers of machines",
		Long:              "List the host helper processes of all the machines, or of the given machines, and whether they are running.",
		RunE:              helpersLs,
		ValidArgsFunction: completion.AutocompleteNone,
		Example: `podman system helpers ls
podman system helpers ls --format json podman-machine-default`,
	}

	helpersStartCmd = &cobra.Command{
		Use:   "start [options] [MACHINE...]",
		Short: "Start the host helpers of running machines",
		Long: `Start the host helper processes that aren't running for all the running machines, or for the given machines.

This restores a running machine whose helpers have been stopped or killed, for example by an upgrade of podman.`,
		RunE:              helpersStart,
		ValidArgsFunction: completion.AutocompleteNone,
		Example: `podman system helpers start
podman system helpers start --helper win-sshproxy podman-machine-default`,
	}

	helpersStopCmd = &cobra.Command{
		Use:   "stop [options] [MACHINE...]",
		Short: "Stop the host helpers of machines",
		Long: `Stop the host helper processes of all the machines, or of the given machines.

The machines keep running, but the features provided by the stopped helpers (e.g. API forwarding) are unavailable until the helpers are started again.`,
		RunE:              helpersStop,
		ValidArgsFunction: completion.AutocompleteNone,
		Example: `podman system helpers stop
podman system helpers stop --helper gvproxy podman-machine-default`,
	}

	helpersOpts      shim.HelpersOptions
	helpersFormat    string
	helpersNoHeading bool
)

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: helpersCmd,
		Parent:  systemCmd,
	})
	for _, c := range []*cobra.Command{helpersLsCmd, helpersStartCmd, helpersStopCmd} {
		registry.Commands = append(registry.Commands, registry.CliCommand{
			Command: c,
			Parent:  helpersCmd,
		})
		helperFlagName := "helper"
		c.Flags().StringSliceVar(&helpersOpts.Helpers, helperFlagName, nil, "Only act on the given helpers (gvproxy, win-sshproxy, server9p)")
		_ = c.RegisterFlagCompletionFunc(helperFlagName, completion.AutocompleteDefault)
	}

	flags := helpersLsCmd.Flags()
	formatFlagName := "format"
	flags.StringVar(&helpersFormat, formatFlagName, "{{range .}}{{.Machine}}\t{{.VMType}}\t{{.MachineState}}\t{{.Name}}\t{{.PID}}\t{{.Status}}\n{{end -}}", "Format helpers output using JSON or a Go template")
	_ = helpersLsCmd.RegisterFlagCompletionFunc(formatFlagName, common.AutocompleteFormat(&helperReport{}))
	flags.BoolVarP(&helpersNoHeading, "noheading", "n", false, "Do not print headers")
}

type helperReport struct {
	Machine      string
	VMType       string
	MachineState string
	Name         string
	PID          int
	Running      bool
	Status       string
}

func toHelperReports(infos []shim.HelperInfo) []helperReport {
	reports := make([]helperReport, 0, len(infos))
	for _, i := range infos {
		status := "stopped"
		switch {
		case i.Running:
			status = "running"
		case i.MachineState == "running":
			// The machine is running but is missing this helper
			status = "missing"
		}
		reports = append(reports, helperReport{
			Machine:      i.Machine,
			VMType:       i.VMType,
			MachineState: i.MachineState,
			Name:         i.Name,
			PID:          i.PID,
			Running:      i.Running,
			Status:       status,
		})
	}
	return reports
}

func helpersLs(cmd *cobra.Command, args []string) error {
	helpersOpts.Machines = args
	infos, err := shim.ListHelpers(provider2.GetAll(), helpersOpts)
	if err != nil {
		return err
	}
	reports := toHelperReports(infos)

	if report.IsJSON(helpersFormat) {
		b, err := json.MarshalIndent(reports, "", "    ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}

	headers := report.Headers(helperReport{}, map[string]string{
		"VMType":       "VM TYPE",
		"MachineState": "MACHINE STATE",
		"Name":         "HELPER",
	})
	rpt := report.New(os.Stdout, cmd.Name())
	defer rpt.Flush()

	origin := report.OriginPodman
	if cmd.Flag("format").Changed {
		origin = report.OriginUser
	}
	rpt, err = rpt.Parse(origin, helpersFormat)
	if err != nil {
		return err
	}
	if rpt.RenderHeaders && !helpersNoHeading {
		if err := rpt.Execute(headers); err != nil {
			return fmt.Errorf("failed to write report column headers: %w", err)
		}
	}
	return rpt.Execute(reports)
}

func helpersStart(_ *cobra.Command, args []string) error {
	helpersOpts.Machines = args
	started, err := shim.StartHelpers(provider2.GetAll(), helpersOpts)
	for _, h := range started {
		fmt.Printf("Started %s for machine %q (PID %d)\n", h.Name, h.Machine, h.PID)
	}
	return err
}

func helpersStop(_ *cobra.Command, args []string) error {
	helpersOpts.Machines = args
	stopped, err := shim.StopHelpers(provider2.GetAll(), helpersOpts)
	for _, h := range stopped {
		fmt.Printf("Stopped %s for machine %q (PID %d)\n", h.Name, h.Machine, h.PID)
	}
	return err
}
