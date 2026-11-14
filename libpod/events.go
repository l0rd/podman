//go:build !remote && (linux || freebsd)

package libpod

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
	"go.podman.io/podman/v6/libpod/define"
	"go.podman.io/podman/v6/libpod/events"
)

// newEventer returns an eventer that can be used to read/write events
func (r *Runtime) newEventer() (events.Eventer, error) {
	if r.config.Engine.EventsLogFilePath == "" {
		// default, use path under tmpdir when none was explicitly set by the user
		r.config.Engine.EventsLogFilePath = filepath.Join(r.config.Engine.TmpDir, "events", "events.log")
	}
	options := events.EventerOptions{
		EventerType:    r.config.Engine.EventsLogger,
		LogFilePath:    r.config.Engine.EventsLogFilePath,
		LogFileMaxSize: r.config.Engine.EventsLogMaxSize(),
	}
	return events.NewEventer(options)
}

// newContainerEvent creates a new event based on a container
func (c *Container) newContainerEvent(status events.Status) {
	if err := c.newContainerEventWithInspectData(status, define.HealthCheckResults{}, false, false, nil); err != nil {
		logrus.Errorf("Unable to write container event: %v", err)
	}
}

// newContainerKillEvent creates a new kill event that records the signal sent
func (c *Container) newContainerKillEvent(signal uint) {
	attributes := map[string]string{"signal": strconv.FormatUint(uint64(signal), 10)}
	if err := c.newContainerEventWithInspectData(events.Kill, define.HealthCheckResults{}, false, false, attributes); err != nil {
		logrus.Errorf("Unable to write container event: %v", err)
	}
}

// newContainerHealthCheckEvent creates a new healthcheck event with the given status
func (c *Container) newContainerHealthCheckEvent(healthCheckResult define.HealthCheckResults, statusChanged bool) {
	if err := c.newContainerEventWithInspectData(events.HealthStatus, healthCheckResult, statusChanged, false, nil); err != nil {
		logrus.Errorf("Unable to write container event: %v", err)
	}
}

// newContainerEventWithInspectData creates a new event and sets the
// ContainerInspectData field if inspectData is set. The given attributes
// are added to the container labels in the event attributes.
func (c *Container) newContainerEventWithInspectData(status events.Status, healthCheckResult define.HealthCheckResults, healthStatusChanged, inspectData bool, attributes map[string]string) error {
	e := events.NewEvent(status)
	e.ID = c.ID()
	e.Name = c.Name()
	e.Image = c.config.RootfsImageName
	e.Type = events.Container
	e.HealthStatus = healthCheckResult.Status
	if c.HealthCheckLogDestination() == define.HealthCheckEventsLoggerDestination {
		if len(healthCheckResult.Log) > 0 {
			logData, err := json.Marshal(healthCheckResult.Log[len(healthCheckResult.Log)-1])
			if err != nil {
				return fmt.Errorf("unable to marshall healthcheck log for writing: %w", err)
			}
			e.HealthLog = string(logData)
		}
	}
	e.HealthFailingStreak = healthCheckResult.FailingStreak
	e.HealthStatusChanged = healthStatusChanged

	e.Details = events.Details{
		PodID:      c.PodID(),
		Attributes: c.Labels(),
	}
	maps.Copy(e.Details.Attributes, attributes)

	if inspectData {
		err := func() error {
			data, err := c.inspectLocked(true)
			if err != nil {
				return err
			}
			rawData, err := json.Marshal(data)
			if err != nil {
				return err
			}
			e.Details.ContainerInspectData = string(rawData)
			return nil
		}()
		if err != nil {
			return fmt.Errorf("adding inspect data to container-create event: %w", err)
		}
	}

	if status == events.Remove {
		exitCode, err := c.runtime.state.GetContainerExitCode(c.ID())
		if err == nil {
			intExitCode := int(exitCode)
			e.ContainerExitCode = &intExitCode
		}
	}

	return c.runtime.eventer.Write(e)
}

// newContainerExitedEvent creates a new event for a container's death
func (c *Container) newContainerExitedEvent(exitCode int32) {
	e := events.NewEvent(events.Exited)
	e.ID = c.ID()
	e.Name = c.Name()
	e.Image = c.config.RootfsImageName
	e.Type = events.Container
	e.PodID = c.PodID()
	intExitCode := int(exitCode)
	e.ContainerExitCode = &intExitCode

	attrs := c.Labels()
	// Docker reports how long the container ran, in whole seconds.
	attrs["execDuration"] = strconv.Itoa(int(c.state.FinishedTime.Sub(c.state.StartedTime).Seconds()))
	if c.state.OOMKilled {
		e.OOMKilled = &c.state.OOMKilled
	}
	e.Details = events.Details{Attributes: attrs}

	if err := c.runtime.eventer.Write(e); err != nil {
		logrus.Errorf("Unable to write container exited event: %q", err)
	}
}

// newExecDiedEvent creates a new event for an exec session's death
func (c *Container) newExecDiedEvent(sessionID string, exitCode int) {
	e := events.NewEvent(events.ExecDied)
	e.ID = c.ID()
	e.Name = c.Name()
	e.Image = c.config.RootfsImageName
	e.Type = events.Container
	intExitCode := exitCode
	e.ContainerExitCode = &intExitCode

	attrs := c.Labels()
	attrs["execID"] = sessionID
	e.Attributes = attrs

	if err := c.runtime.eventer.Write(e); err != nil {
		logrus.Errorf("Unable to write exec died event: %q", err)
	}
}

// NewNetworkEvent creates a new event based on a network create/remove
func (r *Runtime) NewNetworkEvent(status events.Status, netName, netID, netDriver string) {
	e := events.NewEvent(status)
	e.Network = netName
	e.ID = netID
	e.Attributes = make(map[string]string)
	e.Attributes["driver"] = netDriver
	e.Type = events.Network
	if err := r.eventer.Write(e); err != nil {
		logrus.Errorf("Unable to write network event: %q", err)
	}
}

// NewNetworkPruneEvent creates a new event for a network prune as a whole
func (r *Runtime) NewNetworkPruneEvent() {
	e := events.NewEvent(events.Prune)
	e.Type = events.Network
	if err := r.eventer.Write(e); err != nil {
		logrus.Errorf("Unable to write network event: %q", err)
	}
}

// newNetworkEvent creates a new event based on a network connect/disconnect.
// The network ID and driver are recorded in the attributes, as the network
// may be gone by the time the event is read.
func (c *Container) newNetworkEvent(status events.Status, netName string) {
	e := events.NewEvent(status)
	e.ID = c.ID()
	e.Name = c.Name()
	e.Type = events.Network
	e.Network = netName
	if net, err := c.runtime.network.NetworkInspect(netName); err == nil {
		e.Attributes = map[string]string{
			"driver":    net.Driver,
			"networkId": net.ID,
		}
	} else {
		logrus.Debugf("Unable to inspect network %s for the %s event: %v", netName, status, err)
	}
	if err := c.runtime.eventer.Write(e); err != nil {
		logrus.Errorf("Unable to write pod event: %q", err)
	}
}

// newPodEvent creates a new event for a libpod pod
func (p *Pod) newPodEvent(status events.Status) {
	e := events.NewEvent(status)
	e.ID = p.ID()
	e.Name = p.Name()
	e.Type = events.Pod
	e.Attributes = p.Labels()
	if err := p.runtime.eventer.Write(e); err != nil {
		logrus.Errorf("Unable to write pod event: %q", err)
	}
}

// NewSystemEvent creates a new event for libpod as a whole.
func (r *Runtime) NewSystemEvent(status events.Status) {
	e := events.NewEvent(status)
	e.Type = events.System

	if err := r.eventer.Write(e); err != nil {
		logrus.Errorf("Unable to write system event: %q", err)
	}
}

// newVolumeEvent creates a new event for a libpod volume
func (v *Volume) newVolumeEvent(status events.Status) {
	v.newVolumeEventWithAttributes(status, nil)
}

// newVolumeMountEvent creates a new event for a volume mounted into a container
func (v *Volume) newVolumeMountEvent(ctr *Container, namedVol *ContainerNamedVolume) {
	v.newVolumeEventWithAttributes(events.Mount, map[string]string{
		"container":   ctr.ID(),
		"destination": namedVol.Dest,
		"read/write":  strconv.FormatBool(!slices.Contains(namedVol.Options, "ro")),
	})
}

// newVolumeUnmountEvent creates a new event for a volume unmounted from a container
func (v *Volume) newVolumeUnmountEvent(ctr *Container) {
	v.newVolumeEventWithAttributes(events.Unmount, map[string]string{"container": ctr.ID()})
}

// newVolumeEventWithAttributes creates a new event for a libpod volume. The
// volume driver and the given attributes are added to the volume labels in
// the event attributes.
func (v *Volume) newVolumeEventWithAttributes(status events.Status, attributes map[string]string) {
	e := events.NewEvent(status)
	e.Name = v.Name()
	e.Type = events.Volume
	e.Attributes = v.Labels()
	e.Attributes["driver"] = v.Driver()
	maps.Copy(e.Attributes, attributes)
	if err := v.runtime.eventer.Write(e); err != nil {
		logrus.Errorf("Unable to write volume event: %q", err)
	}
}

// newVolumePruneEvent creates a new event for a volume prune as a whole,
// with the space it reclaimed
func (r *Runtime) newVolumePruneEvent(reclaimed uint64) {
	e := events.NewEvent(events.Prune)
	e.Type = events.Volume
	e.Attributes = map[string]string{"reclaimed": strconv.FormatUint(reclaimed, 10)}
	if err := r.eventer.Write(e); err != nil {
		logrus.Errorf("Unable to write volume event: %q", err)
	}
}

// NewSecretEvent creates a new event for a libpod secret
func (r *Runtime) NewSecretEvent(status events.Status, secretID string) {
	e := events.NewEvent(status)
	e.ID = secretID
	e.Type = events.Secret
	if err := r.eventer.Write(e); err != nil {
		logrus.Errorf("Unable to write secret event: %q", err)
	}
}

// Events is a wrapper function for everyone to begin tailing the events log
// with options
func (r *Runtime) Events(ctx context.Context, options events.ReadOptions) error {
	return r.eventer.Read(ctx, options)
}

// GetEvents reads the event log and returns events based on input filters
func (r *Runtime) GetEvents(ctx context.Context, filters []string) ([]*events.Event, error) {
	eventChannel := make(chan events.ReadResult)
	options := events.ReadOptions{
		EventChannel: eventChannel,
		Filters:      filters,
		FromStart:    true,
		Stream:       false,
	}

	err := r.eventer.Read(ctx, options)
	if err != nil {
		return nil, err
	}

	logEvents := make([]*events.Event, 0, len(eventChannel))
	for evt := range eventChannel {
		// we ignore any error here, this is only used on the backup
		// GetExecDiedEvent() died path as best effort anyway
		if evt.Error == nil {
			logEvents = append(logEvents, evt.Event)
		}
	}

	return logEvents, nil
}

// GetExecDiedEvent takes a container name or ID, exec session ID, and returns
// that exec session's Died event (if it has already occurred).
func (r *Runtime) GetExecDiedEvent(ctx context.Context, nameOrID, execSessionID string) (*events.Event, error) {
	filters := []string{
		fmt.Sprintf("container=%s", nameOrID),
		"event=exec_died",
		"type=container",
		fmt.Sprintf("label=execID=%s", execSessionID),
	}

	containerEvents, err := r.GetEvents(ctx, filters)
	if err != nil {
		return nil, err
	}
	// There *should* only be one event maximum.
	// But... just in case... let's not blow up if there's more than one.
	if len(containerEvents) < 1 {
		return nil, fmt.Errorf("exec died event for session %s (container %s) not found: %w", execSessionID, nameOrID, events.ErrEventNotFound)
	}
	return containerEvents[len(containerEvents)-1], nil
}

// spawnEventForwarder starts a goroutine that reads events from the given channel, transforms and forwards them to the eventer.
func spawnEventForwarder[T any](eventer events.Eventer, toLibpodEvent func(libEvent T) events.Event, eventChannel <-chan T, shutdownChan chan bool) {
	go func() {
		sawShutdown := false
		for {
			// Make sure to read and write all events before
			// shutting down.
			for len(eventChannel) > 0 {
				libEvent := <-eventChannel
				libpodEvent := toLibpodEvent(libEvent)
				if err := eventer.Write(libpodEvent); err != nil {
					logrus.Errorf("Unable to write event of type %T: %q", libEvent, err)
				}
			}

			if sawShutdown {
				close(shutdownChan)
				return
			}

			select {
			case <-shutdownChan:
				sawShutdown = true
			case <-time.After(100 * time.Millisecond):
			}
		}
	}()
}
