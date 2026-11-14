//go:build !remote && (linux || freebsd)

package compat

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	nettypes "go.podman.io/common/libnetwork/types"
	netutil "go.podman.io/common/libnetwork/util"
	"go.podman.io/podman/v6/libpod"
	"go.podman.io/podman/v6/libpod/define"
	"go.podman.io/podman/v6/pkg/api/handlers/utils"
	api "go.podman.io/podman/v6/pkg/api/types"
	"go.podman.io/podman/v6/pkg/domain/entities"
	"go.podman.io/podman/v6/pkg/domain/infra/abi"
	"go.podman.io/podman/v6/pkg/util"

	dockerNetwork "github.com/moby/moby/api/types/network"
	"github.com/sirupsen/logrus"
)

// leaseRangeToDockerIPRange maps a Libpod LeaseRange back to the Docker IPAM
// IPRange it was derived from, or the zero Prefix when the range is not exactly
// the usable span of a CIDR.
//
// CreateNetwork builds the lease range with FirstIPInSubnet/LastIPInSubnet, so
// StartIP is the network address plus one and EndIP is the broadcast address.
// XORing the network address with EndIP yields the host mask, which identifies
// the prefix length in a single step; requiring that mask to be a run of
// trailing ones, and the network address to be aligned to it, is what rejects
// partial ranges such as 10.0.0.5-10.0.0.99.
func leaseRangeToDockerIPRange(lr *nettypes.LeaseRange) netip.Prefix {
	if lr == nil || lr.StartIP == nil || lr.EndIP == nil {
		return netip.Prefix{}
	}

	start, ok := netip.AddrFromSlice(lr.StartIP)
	if !ok {
		return netip.Prefix{}
	}
	end, ok := netip.AddrFromSlice(lr.EndIP)
	if !ok {
		return netip.Prefix{}
	}

	// Collapse 4-in-6 so both addresses agree on a family and bit length.
	start = start.Unmap()
	end = end.Unmap()
	if start.BitLen() != end.BitLen() || end.Less(start) {
		return netip.Prefix{}
	}

	// A single-address range is a host route; there is no network address below it.
	if start == end {
		return netip.PrefixFrom(start, start.BitLen())
	}

	network := start.Prev()
	if !network.IsValid() {
		// start was the all-zero address, so no network address precedes it.
		return netip.Prefix{}
	}

	hostBits, ok := hostBitsBetween(network, end)
	if !ok {
		return netip.Prefix{}
	}

	// Masked clears the host bits, so a network address that does not survive it
	// was never aligned to the prefix and the range is not a whole CIDR.
	pfx := netip.PrefixFrom(network, network.BitLen()-hostBits)
	if pfx.Masked().Addr() != network {
		return netip.Prefix{}
	}
	return pfx
}

// hostBitsBetween counts the host bits implied by the mask network^end, and
// reports whether that mask is a contiguous run of trailing ones, i.e. of the
// form 0...01...1. Anything else means the range does not line up with a CIDR
// boundary. Both addresses are compared in their 16-byte form, which is safe
// because the caller has already established that they share a family.
func hostBitsBetween(network, end netip.Addr) (int, bool) {
	n, e := network.As16(), end.As16()

	hostBits := 0
	i := 15
	for i >= 0 && n[i]^e[i] == 0xff {
		hostBits += 8
		i--
	}
	if i >= 0 {
		h := n[i] ^ e[i]
		if h&(h+1) != 0 {
			return 0, false
		}
		for h != 0 {
			hostBits++
			h >>= 1
		}
		// Every byte above the mask must match, or the two ends sit in
		// different networks.
		for i--; i >= 0; i-- {
			if n[i] != e[i] {
				return 0, false
			}
		}
	}
	return hostBits, true
}

func normalizeNetworkName(rt *libpod.Runtime, name string) (string, bool) {
	if name == nettypes.BridgeNetworkDriver {
		return rt.Network().DefaultNetworkName(), true
	}
	return name, false
}

// Docker's errors for connecting a container to the host or none network.
var (
	errConflictHostNetwork = errors.New("container cannot be disconnected from host network or connected to host network")
	errConflictNoNetwork   = errors.New("container cannot be connected to multiple networks with one of the networks in private (none) mode")
)

// predefinedNetworks are the networks other than bridge that Docker always
// has. In Podman, host and none are network modes rather than networks, so
// the compat API reports synthetic networks for them. Like the default
// network ID, which is the SHA-256 of "podman", their ID is the SHA-256 of
// their name, so that it never changes and is the same on every host.
var predefinedNetworks = []nettypes.Network{
	newPredefinedNetwork("host", "host"),
	newPredefinedNetwork("none", "null"),
}

func newPredefinedNetwork(name, driver string) nettypes.Network {
	return nettypes.Network{
		Name:   name,
		ID:     fmt.Sprintf("%x", sha256.Sum256([]byte(name))),
		Driver: driver,
	}
}

// findPredefinedNetwork returns the predefined network with the name, ID or
// ID prefix nameOrID, or nil if there is none or if nameOrID is the ID prefix
// of a real network.
func findPredefinedNetwork(rt *libpod.Runtime, nameOrID string) *nettypes.Network {
	if nameOrID == "" {
		return nil
	}
	for i := range predefinedNetworks {
		network := &predefinedNetworks[i]
		if network.Name == nameOrID {
			return network
		}
		if strings.HasPrefix(network.ID, nameOrID) {
			if _, err := rt.Network().NetworkInspect(nameOrID); err == nil {
				return nil
			}
			return network
		}
	}
	return nil
}

// listPredefinedNetworks returns the predefined networks that match filters.
func listPredefinedNetworks(filters map[string][]string) ([]nettypes.Network, error) {
	filters = maps.Clone(filters)
	if val, ok := filters["dangling"]; ok {
		// Docker never reports a predefined network as dangling. Invalid
		// values are reported by the network list.
		if len(val) == 1 {
			if dangling, err := strconv.ParseBool(val[0]); err == nil && dangling {
				return nil, nil
			}
		}
		delete(filters, "dangling")
	}
	filterFuncs, err := netutil.GenerateNetworkFilters(filters)
	if err != nil {
		return nil, err
	}
	var networks []nettypes.Network
	for _, network := range predefinedNetworks {
		if !slices.ContainsFunc(filterFuncs, func(match nettypes.FilterFunc) bool { return !match(network) }) {
			networks = append(networks, network)
		}
	}
	return networks, nil
}

// convertPredefinedNetworkToDockerNetwork returns the Docker network of a
// predefined network, with the given container endpoints.
func convertPredefinedNetworkToDockerNetwork(network *nettypes.Network, containers map[string]dockerNetwork.EndpointResource) *dockerNetwork.Inspect {
	return &dockerNetwork.Inspect{
		Network: dockerNetwork.Network{
			Name:    network.Name,
			ID:      network.ID,
			Driver:  network.Driver,
			Scope:   "local",
			IPAM:    dockerNetwork.IPAM{Driver: "default"},
			Options: map[string]string{},
			Labels:  map[string]string{},
		},
		Containers: containers,
	}
}

// inspectPredefinedNetwork returns the Docker network of a predefined network,
// with the running containers that use its network mode as endpoints.
func inspectPredefinedNetwork(rt *libpod.Runtime, network *nettypes.Network) (*dockerNetwork.Inspect, error) {
	ctrs, err := rt.GetRunningContainers()
	if err != nil {
		return nil, err
	}
	containers := make(map[string]dockerNetwork.EndpointResource)
	for _, ctr := range ctrs {
		if ctr.NetworkMode() == network.Name {
			containers[ctr.ID()] = dockerNetwork.EndpointResource{Name: ctr.Name()}
		}
	}
	return convertPredefinedNetworkToDockerNetwork(network, containers), nil
}

func InspectNetwork(w http.ResponseWriter, r *http.Request) {
	runtime := r.Context().Value(api.RuntimeKey).(*libpod.Runtime)

	// scope is only used to see if the user passes any illegal value, verbose is not used but implemented
	// for compatibility purposes only.
	query := struct {
		scope   string `schema:"scope"`
		verbose bool   `schema:"verbose"`
	}{
		scope: "local",
	}
	decoder := utils.GetDecoder(r)
	if err := decoder.Decode(&query, r.URL.Query()); err != nil {
		utils.Error(w, http.StatusBadRequest, fmt.Errorf("failed to parse parameters for %s: %w", r.URL.String(), err))
		return
	}

	if query.scope != "local" {
		utils.Error(w, http.StatusBadRequest, define.ErrInvalidArg)
		return
	}
	name, changed := normalizeNetworkName(runtime, utils.GetName(r))
	net, err := runtime.Network().NetworkInspect(name)
	if err != nil {
		if predefined := findPredefinedNetwork(runtime, name); predefined != nil && errors.Is(err, define.ErrNoSuchNetwork) {
			report, err := inspectPredefinedNetwork(runtime, predefined)
			if err != nil {
				utils.InternalServerError(w, err)
				return
			}
			utils.WriteResponse(w, http.StatusOK, report)
			return
		}
		utils.NetworkNotFound(w, name, err)
		return
	}
	ic := abi.ContainerEngine{Libpod: runtime}
	statuses, err := ic.GetContainerNetStatuses()
	if err != nil {
		utils.InternalServerError(w, err)
		return
	}
	report, err := convertLibpodNetworktoDockerNetwork(runtime, statuses, &net, changed)
	if err != nil {
		utils.InternalServerError(w, err)
		return
	}
	utils.WriteResponse(w, http.StatusOK, report)
}

func convertLibpodNetworktoDockerNetwork(runtime *libpod.Runtime, statuses []abi.ContainerNetStatus, network *nettypes.Network, changeDefaultName bool) (*dockerNetwork.Inspect, error) {
	containerEndpoints := make(map[string]dockerNetwork.EndpointResource, len(statuses))
	for _, st := range statuses {
		if netData, ok := st.Status[network.Name]; ok {
			var err error
			var ipv4_pfx netip.Prefix
			var ipv6_pfx netip.Prefix
			macAddr := nettypes.HardwareAddr{}
			for _, dev := range netData.Interfaces {
				for _, subnet := range dev.Subnets {
					// Note the docker API really wants the full CIDR subnet not just a single ip.
					// https://github.com/containers/podman/pull/12328
					if netutil.IsIPv4(subnet.IPNet.IP) {
						ipv4_pfx, err = netip.ParsePrefix(subnet.IPNet.String())
						if err != nil {
							return nil, fmt.Errorf("invalid IPv4Address %q: %w", subnet.IPNet.String(), err)
						}
					} else {
						ipv6_pfx, err = netip.ParsePrefix(subnet.IPNet.String())
						if err != nil {
							return nil, fmt.Errorf("invalid IPv6Address %q: %w", subnet.IPNet.String(), err)
						}
					}
				}
				macAddr = dev.MacAddress
				break
			}

			containerEndpoint := dockerNetwork.EndpointResource{
				Name:        st.Name,
				MacAddress:  dockerNetwork.HardwareAddr(net.HardwareAddr(macAddr)),
				IPv4Address: ipv4_pfx,
				IPv6Address: ipv6_pfx,
			}
			containerEndpoints[st.ID] = containerEndpoint
		}
	}
	ipamConfigs := make([]dockerNetwork.IPAMConfig, 0, len(network.Subnets))
	for _, sub := range network.Subnets {
		subnet, err := netip.ParsePrefix(sub.Subnet.String())
		if err != nil {
			return nil, err
		}
		gateway, ok := netip.AddrFromSlice(sub.Gateway)
		if !ok {
			return nil, fmt.Errorf("invalid gateway IP %v", sub.Gateway)
		}
		ipamConfig := dockerNetwork.IPAMConfig{
			Subnet:  subnet,
			Gateway: gateway,
			IPRange: leaseRangeToDockerIPRange(sub.LeaseRange),
		}
		ipamConfigs = append(ipamConfigs, ipamConfig)
	}
	ipamDriver := network.IPAMOptions["driver"]
	if ipamDriver == nettypes.HostLocalIPAMDriver {
		ipamDriver = "default"
	}
	ipam := dockerNetwork.IPAM{
		Driver:  ipamDriver,
		Options: network.IPAMOptions,
		Config:  ipamConfigs,
	}

	name := network.Name
	if changeDefaultName && name == runtime.Network().DefaultNetworkName() {
		name = nettypes.BridgeNetworkDriver
	}

	// Networks created by CreateNetwork keep the options as the client sent them.
	options := network.Options
	if network.OpaqueOptions != nil {
		options = network.OpaqueOptions
	}

	report := dockerNetwork.Inspect{
		Network: dockerNetwork.Network{
			Name:       name,
			ID:         network.ID,
			Driver:     network.Driver,
			Created:    network.Created,
			Scope:      "local",
			EnableIPv4: true,
			EnableIPv6: network.IPv6Enabled,
			IPAM:       ipam,
			Internal:   network.Internal,
			Attachable: false,
			Ingress:    false,
			ConfigFrom: dockerNetwork.ConfigReference{},
			ConfigOnly: false,
			Options:    options,
			Labels:     network.Labels,
			Peers:      nil,
		},
		Containers: containerEndpoints,
		Services:   nil,
	}
	return &report, nil
}

func ListNetworks(w http.ResponseWriter, r *http.Request) {
	runtime := r.Context().Value(api.RuntimeKey).(*libpod.Runtime)
	filterMap, err := util.PrepareFilters(r)
	if err != nil {
		utils.Error(w, http.StatusInternalServerError, fmt.Errorf("failed to parse parameters for %s: %w", r.URL.String(), err))
		return
	}

	options := entities.NetworkListOptions{
		Filters: *filterMap,
	}

	predefinedNets, err := listPredefinedNetworks(*filterMap)
	if err != nil {
		utils.InternalServerError(w, err)
		return
	}
	ic := abi.ContainerEngine{Libpod: runtime}
	nets, err := ic.NetworkList(r.Context(), options)
	if err != nil {
		utils.InternalServerError(w, err)
		return
	}
	statuses, err := ic.GetContainerNetStatuses()
	if err != nil {
		utils.InternalServerError(w, err)
		return
	}
	reports := make([]*dockerNetwork.Summary, 0, len(nets))
	for _, net := range nets {
		report, err := convertLibpodNetworktoDockerNetwork(runtime, statuses, &net, true)
		if err != nil {
			utils.InternalServerError(w, err)
			return
		}
		reports = append(reports, &dockerNetwork.Summary{
			Network: report.Network,
		})
	}
	for _, net := range predefinedNets {
		reports = append(reports, &dockerNetwork.Summary{
			Network: convertPredefinedNetworkToDockerNetwork(&net, nil).Network,
		})
	}
	utils.WriteResponse(w, http.StatusOK, reports)
}

func CreateNetwork(w http.ResponseWriter, r *http.Request) {
	var (
		networkCreate dockerNetwork.CreateRequest
		network       nettypes.Network
	)
	runtime := r.Context().Value(api.RuntimeKey).(*libpod.Runtime)

	if err := utils.ReadJSONFromBody(r, &networkCreate); err != nil {
		utils.Error(w, http.StatusBadRequest, err)
		return
	}

	network.Name = networkCreate.Name
	if networkCreate.Driver == "" {
		networkCreate.Driver = nettypes.DefaultNetworkDriver
	}
	network.Driver = networkCreate.Driver
	network.Labels = networkCreate.Labels
	network.Internal = networkCreate.Internal
	if networkCreate.EnableIPv6 != nil {
		network.IPv6Enabled = *networkCreate.EnableIPv6
	}

	network.Options = make(map[string]string)
	// Docker stores all the options as given and returns them on inspect,
	// including the ones the driver doesn't use. Keep them all as opaque
	// options, and translate the ones netavark knows to netavark options.
	network.OpaqueOptions = networkCreate.Options

	for opt, optVal := range networkCreate.Options {
		switch opt {
		case nettypes.MTUOption:
			fallthrough
		case "com.docker.network.driver.mtu":
			network.Options[nettypes.MTUOption] = optVal
		case "com.docker.network.bridge.name":
			if network.Driver == nettypes.BridgeNetworkDriver {
				network.NetworkInterface = optVal
			}
		case nettypes.ModeOption:
			if network.Driver == nettypes.MacVLANNetworkDriver || network.Driver == nettypes.IPVLANNetworkDriver {
				network.Options[opt] = optVal
			}
		case "parent":
			if network.Driver == nettypes.MacVLANNetworkDriver || network.Driver == nettypes.IPVLANNetworkDriver {
				network.NetworkInterface = optVal
			}
		}
	}

	// dns is only enabled for the bridge driver
	if network.Driver == nettypes.BridgeNetworkDriver {
		network.DNSEnabled = true
	}

	if networkCreate.IPAM != nil && len(networkCreate.IPAM.Config) > 0 {
		for _, conf := range networkCreate.IPAM.Config {
			s := nettypes.Subnet{}
			if conf.Subnet.IsValid() {
				pfx := conf.Subnet.Masked()
				addr := pfx.Addr()

				s.Subnet = nettypes.IPNet{
					IPNet: net.IPNet{
						IP:   net.IP(addr.AsSlice()),
						Mask: net.CIDRMask(pfx.Bits(), addr.BitLen()),
					},
				}
			}
			if conf.Gateway.IsValid() {
				s.Gateway = net.IP(conf.Gateway.AsSlice())
			}
			if conf.IPRange.IsValid() {
				if conf.Subnet.IsValid() {
					inSubnet := conf.IPRange.Bits() >= conf.Subnet.Bits() && conf.Subnet.Contains(conf.IPRange.Addr())
					if !inSubnet {
						utils.Error(w, http.StatusBadRequest,
							fmt.Errorf("invalid ip-range %s: parent subnet %s doesn't contain ip-range", conf.IPRange, conf.Subnet))
						return
					}
				}
				_, net, err := net.ParseCIDR(conf.IPRange.String())
				if err != nil {
					utils.InternalServerError(w, fmt.Errorf("failed to parse ip range: %w", err))
					return
				}
				startIP, err := netutil.FirstIPInSubnet(net)
				if err != nil {
					utils.InternalServerError(w, fmt.Errorf("failed to get first ip in range: %w", err))
					return
				}
				lastIP, err := netutil.LastIPInSubnet(net)
				if err != nil {
					utils.InternalServerError(w, fmt.Errorf("failed to get last ip in range: %w", err))
					return
				}
				s.LeaseRange = &nettypes.LeaseRange{
					StartIP: startIP,
					EndIP:   lastIP,
				}
			}
			network.Subnets = append(network.Subnets, s)
		}
		// FIXME can we use the IPAM driver and options?
	}

	opts := nettypes.NetworkCreateOptions{
		// networkCreate.CheckDuplicate is deprecated since API v1.44,
		// but it defaults to true when sent by the client package to
		// older daemons.
		IgnoreIfExists: false,
	}
	ic := abi.ContainerEngine{Libpod: runtime}
	newNetwork, err := ic.NetworkCreate(r.Context(), network, &opts)
	if err != nil {
		if errors.Is(err, nettypes.ErrNetworkExists) {
			utils.Error(w, http.StatusConflict, err)
		} else {
			utils.InternalServerError(w, err)
		}
		return
	}

	body := struct {
		ID      string `json:"Id"`
		Warning string `json:"Warning"`
	}{
		ID: newNetwork.ID,
	}
	utils.WriteResponse(w, http.StatusCreated, body)
}

func RemoveNetwork(w http.ResponseWriter, r *http.Request) {
	runtime := r.Context().Value(api.RuntimeKey).(*libpod.Runtime)
	ic := abi.ContainerEngine{Libpod: runtime}

	query := struct {
		Force   bool  `schema:"force"`
		Timeout *uint `schema:"timeout"`
	}{
		// This is where you can override the golang default value for one of fields
	}

	decoder := utils.GetDecoder(r)
	if err := decoder.Decode(&query, r.URL.Query()); err != nil {
		utils.Error(w, http.StatusBadRequest, fmt.Errorf("failed to parse parameters for %s: %w", r.URL.String(), err))
		return
	}

	options := entities.NetworkRmOptions{
		Force:   query.Force,
		Timeout: query.Timeout,
	}

	name, changed := normalizeNetworkName(runtime, utils.GetName(r))
	if predefined := findPredefinedNetwork(runtime, name); predefined != nil {
		utils.Error(w, http.StatusForbidden, fmt.Errorf("%s is a pre-defined network and cannot be removed", predefined.Name))
		return
	}
	if network, err := runtime.Network().NetworkInspect(name); err == nil && network.Name == runtime.Network().DefaultNetworkName() {
		if changed {
			name = nettypes.BridgeNetworkDriver
		}
		utils.Error(w, http.StatusForbidden, fmt.Errorf("%s is a pre-defined network and cannot be removed", name))
		return
	}
	if !query.Force {
		var err error
		name, err = disconnectStoppedContainers(r.Context(), runtime, name)
		if err != nil {
			switch {
			case errors.Is(err, define.ErrNoSuchNetwork):
				utils.Error(w, http.StatusNotFound, err)
			case errors.Is(err, define.ErrNetworkInUse):
				utils.Error(w, http.StatusForbidden, err)
			default:
				utils.InternalServerError(w, err)
			}
			return
		}
	}
	reports, err := ic.NetworkRm(r.Context(), []string{name}, options)
	if err != nil {
		utils.Error(w, http.StatusInternalServerError, err)
		return
	}
	if len(reports) == 0 {
		utils.Error(w, http.StatusInternalServerError, fmt.Errorf("internal error"))
		return
	}
	report := reports[0]
	if report.Err != nil {
		if errors.Is(report.Err, define.ErrNoSuchNetwork) {
			utils.Error(w, http.StatusNotFound, define.ErrNoSuchNetwork)
			return
		}
		utils.InternalServerError(w, report.Err)
		return
	}

	utils.WriteResponse(w, http.StatusNoContent, nil)
}

// disconnectStoppedContainers disconnects all the containers that are not
// running from the network nameOrID, so that the network can be removed, and
// returns the network name. Docker only refuses to remove a network that has
// active endpoints: if a container that uses the network is running,
// disconnectStoppedContainers disconnects none and fails with ErrNetworkInUse.
func disconnectStoppedContainers(ctx context.Context, runtime *libpod.Runtime, nameOrID string) (string, error) {
	network, err := runtime.Network().NetworkInspect(nameOrID)
	if err != nil {
		return "", err
	}
	ctrs, err := runtime.GetAllContainers()
	if err != nil {
		return "", err
	}
	var stopped []*libpod.Container
	for _, ctr := range ctrs {
		networks, err := ctr.Networks()
		if errors.Is(err, define.ErrNoSuchCtr) || errors.Is(err, define.ErrCtrRemoved) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !slices.Contains(networks, network.Name) {
			continue
		}
		state, err := ctr.State()
		if err != nil {
			return "", err
		}
		switch state {
		case define.ContainerStateConfigured, define.ContainerStateCreated, define.ContainerStateStopped, define.ContainerStateExited:
			stopped = append(stopped, ctr)
		default:
			return "", fmt.Errorf("error while removing network: network %s id %s has active endpoints%.0w", network.Name, network.ID, define.ErrNetworkInUse)
		}
	}
	for _, ctr := range stopped {
		// A stopped container still has its network set up until it is
		// cleaned up, and disconnect would leave it behind.
		if err := ctr.Cleanup(ctx, true); err != nil && !errors.Is(err, define.ErrCtrStateInvalid) {
			return "", err
		}
		if err := ctr.NetworkDisconnect(ctr.ID(), network.Name, false); err != nil {
			return "", err
		}
	}
	return network.Name, nil
}

// checkEndpointSettings rejects the endpoint settings that Podman cannot apply.
// Static link-local addresses are not supported by the network backend.
func checkEndpointSettings(endpoint *dockerNetwork.EndpointSettings) error {
	if endpoint != nil && endpoint.IPAMConfig != nil && len(endpoint.IPAMConfig.LinkLocalIPs) > 0 {
		return fmt.Errorf("IPAMConfig.LinkLocalIPs is not supported: %w", define.ErrInvalidArg)
	}
	return nil
}

// Connect adds a container to a network
func Connect(w http.ResponseWriter, r *http.Request) {
	runtime := r.Context().Value(api.RuntimeKey).(*libpod.Runtime)

	var netConnect dockerNetwork.ConnectRequest
	if err := utils.ReadJSONFromBody(r, &netConnect); err != nil {
		utils.Error(w, http.StatusBadRequest, err)
		return
	}

	if err := checkEndpointSettings(netConnect.EndpointConfig); err != nil {
		utils.Error(w, http.StatusBadRequest, err)
		return
	}

	netOpts := nettypes.PerNetworkOptions{}

	name, _ := normalizeNetworkName(runtime, utils.GetName(r))
	if predefined := findPredefinedNetwork(runtime, name); predefined != nil {
		if _, err := runtime.LookupContainer(netConnect.Container); err != nil {
			utils.ContainerNotFound(w, netConnect.Container, err)
			return
		}
		err := errConflictHostNetwork
		if predefined.Name == "none" {
			err = errConflictNoNetwork
		}
		utils.Error(w, http.StatusBadRequest, err)
		return
	}
	if netConnect.EndpointConfig != nil {
		if netConnect.EndpointConfig.Aliases != nil {
			netOpts.Aliases = netConnect.EndpointConfig.Aliases
		}
		netOpts.Links = netConnect.EndpointConfig.Links
		netOpts.Options = netConnect.EndpointConfig.DriverOpts

		if netConnect.EndpointConfig.IPAMConfig != nil {
			ipam := netConnect.EndpointConfig.IPAMConfig

			// IPv4
			if ipam.IPv4Address.IsValid() {
				netOpts.StaticIPs = append(
					netOpts.StaticIPs,
					net.IP(ipam.IPv4Address.AsSlice()),
				)
			}

			// IPv6
			if ipam.IPv6Address.IsValid() {
				netOpts.StaticIPs = append(
					netOpts.StaticIPs,
					net.IP(ipam.IPv6Address.AsSlice()),
				)
			}
		}

		// If MAC address is provided
		if len(netConnect.EndpointConfig.MacAddress) != 0 {
			netOpts.StaticMAC = nettypes.HardwareAddr(
				net.HardwareAddr(netConnect.EndpointConfig.MacAddress),
			)
		}
	}
	err := runtime.ConnectContainerToNetwork(netConnect.Container, name, netOpts)
	if err != nil {
		if errors.Is(err, define.ErrNoSuchCtr) {
			utils.ContainerNotFound(w, netConnect.Container, err)
			return
		}
		if errors.Is(err, define.ErrNoSuchNetwork) {
			utils.Error(w, http.StatusNotFound, err)
			return
		}
		if errors.Is(err, define.ErrNetworkConnected) {
			utils.Error(w, http.StatusForbidden, err)
			return
		}
		utils.Error(w, http.StatusInternalServerError, err)
		return
	}
	utils.WriteResponse(w, http.StatusOK, "OK")
}

// Disconnect removes a container from a network
func Disconnect(w http.ResponseWriter, r *http.Request) {
	runtime := r.Context().Value(api.RuntimeKey).(*libpod.Runtime)

	var netDisconnect dockerNetwork.DisconnectRequest
	if err := utils.ReadJSONFromBody(r, &netDisconnect); err != nil {
		utils.Error(w, http.StatusBadRequest, err)
		return
	}

	name, _ := normalizeNetworkName(runtime, utils.GetName(r))
	if predefined := findPredefinedNetwork(runtime, name); predefined != nil {
		ctr, err := runtime.LookupContainer(netDisconnect.Container)
		if err != nil {
			utils.Error(w, http.StatusNotFound, err)
			return
		}
		switch {
		case ctr.NetworkMode() != predefined.Name:
			utils.Error(w, http.StatusInternalServerError, fmt.Errorf("container %s is not connected to the network %s", ctr.ID(), predefined.Name))
		case predefined.Name == "host":
			utils.Error(w, http.StatusBadRequest, errConflictHostNetwork)
		default:
			// Podman can't change the network mode of a container.
			utils.Error(w, http.StatusBadRequest, fmt.Errorf("container %s cannot be disconnected from network %s: %w", ctr.ID(), predefined.Name, define.ErrNetworkModeInvalid))
		}
		return
	}
	err := runtime.DisconnectContainerFromNetwork(netDisconnect.Container, name, netDisconnect.Force)
	if err != nil {
		if errors.Is(err, define.ErrNoSuchCtr) {
			utils.Error(w, http.StatusNotFound, err)
			return
		}
		if errors.Is(err, define.ErrNoSuchNetwork) {
			utils.Error(w, http.StatusNotFound, err)
			return
		}
		utils.Error(w, http.StatusInternalServerError, err)
		return
	}
	utils.WriteResponse(w, http.StatusOK, "OK")
}

// Prune removes unused networks
func Prune(w http.ResponseWriter, r *http.Request) {
	runtime := r.Context().Value(api.RuntimeKey).(*libpod.Runtime)
	filterMap, err := util.PrepareFilters(r)
	if err != nil {
		utils.Error(w, http.StatusInternalServerError, fmt.Errorf("Decode(): %w", err))
		return
	}

	ic := abi.ContainerEngine{Libpod: runtime}
	pruneOptions := entities.NetworkPruneOptions{
		Filters: *filterMap,
	}
	pruneReports, err := ic.NetworkPrune(r.Context(), pruneOptions)
	if err != nil {
		utils.Error(w, http.StatusInternalServerError, err)
		return
	}
	type response struct {
		NetworksDeleted []string
	}
	prunedNetworks := []string{}
	for _, pr := range pruneReports {
		if pr.Error != nil {
			logrus.Error(pr.Error)
			continue
		}
		prunedNetworks = append(prunedNetworks, pr.Name)
	}
	utils.WriteResponse(w, http.StatusOK, response{NetworksDeleted: prunedNetworks})
}
