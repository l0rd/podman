//go:build !remote && (linux || freebsd)

package compat

import (
	"fmt"
	"net/http"

	jsoniter "github.com/json-iterator/go"
	"github.com/sirupsen/logrus"
	"go.podman.io/podman/v6/libpod"
	"go.podman.io/podman/v6/libpod/events"
	"go.podman.io/podman/v6/pkg/api/handlers/utils"
	"go.podman.io/podman/v6/pkg/api/handlers/utils/apiutil"
	api "go.podman.io/podman/v6/pkg/api/types"
	"go.podman.io/podman/v6/pkg/domain/entities"
	"go.podman.io/podman/v6/pkg/util"
)

// toDockerNetworkEvent rewrites a network event the way Docker reports it.
// The actor is the network, with only its name and driver as attributes, and
// connect and disconnect events add the container. Docker calls a removal
// "destroy", and reports a prune as a whole, with no reclaimed space.
func toDockerNetworkEvent(libpodEvent *events.Event, e *entities.Event) {
	attributes := map[string]string{
		"name": libpodEvent.Network,
		"type": libpodEvent.Attributes["driver"],
	}
	switch libpodEvent.Status {
	case events.NetworkConnect, events.NetworkDisconnect:
		attributes["container"] = libpodEvent.ID
		e.Actor.ID = libpodEvent.Attributes["networkId"]
	case events.Remove:
		e.Action = "destroy"
	case events.Prune:
		attributes = map[string]string{"reclaimed": "0"}
	}
	e.Actor.Attributes = attributes
	// Docker sets the deprecated fields only on container and image events.
	e.Status = "" //nolint:staticcheck // deprecated field
	e.ID = ""     //nolint:staticcheck // deprecated field
	e.From = ""   //nolint:staticcheck // deprecated field
}

// toDockerVolumeEvent rewrites a volume event the way Docker reports it. The
// actor is the volume name, with only its driver as attribute, and mount and
// unmount events add the container and, on mount, where and how the volume is
// mounted. Docker calls a removal "destroy", and reports a prune only as a
// whole, with the reclaimed space. It returns false for an event that Docker
// does not write.
func toDockerVolumeEvent(libpodEvent *events.Event, e *entities.Event) bool {
	attributes := map[string]string{"driver": libpodEvent.Attributes["driver"]}
	switch libpodEvent.Status {
	case events.Mount:
		attributes["container"] = libpodEvent.Attributes["container"]
		attributes["destination"] = libpodEvent.Attributes["destination"]
		attributes["read/write"] = libpodEvent.Attributes["read/write"]
		// Docker does not support a propagation mode on volume mounts.
		attributes["propagation"] = ""
	case events.Unmount:
		attributes["container"] = libpodEvent.Attributes["container"]
	case events.Remove:
		e.Action = "destroy"
	case events.Prune:
		// A pruned volume already has its removal event.
		if libpodEvent.Name != "" {
			return false
		}
		attributes = map[string]string{"reclaimed": libpodEvent.Attributes["reclaimed"]}
	}
	e.Actor.ID = libpodEvent.Name
	e.Actor.Attributes = attributes
	// Docker sets the deprecated fields only on container and image events.
	e.Status = "" //nolint:staticcheck // deprecated field
	e.ID = ""     //nolint:staticcheck // deprecated field
	e.From = ""   //nolint:staticcheck // deprecated field
	return true
}

// GetEvents endpoint serves both the docker-compatible one and the new libpod one
func GetEvents(w http.ResponseWriter, r *http.Request) {
	var (
		fromStart bool
		decoder   = utils.GetDecoder(r)
		runtime   = r.Context().Value(api.RuntimeKey).(*libpod.Runtime)
		json      = jsoniter.ConfigCompatibleWithStandardLibrary // FIXME: this should happen on the package level
	)

	// NOTE: the "filters" parameter is extracted separately for backwards
	// compat via `filterFromRequest()`.
	query := struct {
		Since  string `schema:"since"`
		Until  string `schema:"until"`
		Stream bool   `schema:"stream"`
	}{
		Stream: true,
	}
	if err := decoder.Decode(&query, r.URL.Query()); err != nil {
		utils.Error(w, http.StatusBadRequest, fmt.Errorf("failed to parse parameters for %s: %w", r.URL.String(), err))
		return
	}

	if len(query.Since) > 0 || len(query.Until) > 0 {
		fromStart = true
	}

	libpodFilters, err := util.FiltersFromRequest(r)
	if err != nil {
		utils.Error(w, http.StatusBadRequest, fmt.Errorf("failed to parse filters for %s: %w", r.URL.String(), err))
		return
	}
	eventChannel := make(chan events.ReadResult)

	readOpts := events.ReadOptions{
		FromStart:    fromStart,
		Stream:       query.Stream,
		Filters:      libpodFilters,
		EventChannel: eventChannel,
		Since:        query.Since,
		Until:        query.Until,
	}
	err = runtime.Events(r.Context(), readOpts)
	if err != nil {
		utils.InternalServerError(w, err)
		return
	}

	flush := func() {}
	if flusher, ok := w.(http.Flusher); ok {
		flush = flusher.Flush
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	flush()

	coder := json.NewEncoder(w)
	coder.SetEscapeHTML(true)

	for {
		select {
		case <-r.Context().Done():
			return
		case evt, ok := <-eventChannel:
			if !ok {
				return
			}
			if evt.Error != nil {
				logrus.Errorf("Unable to read event: %q", evt.Error)
				continue
			}
			if evt.Event == nil {
				continue
			}

			e := entities.ConvertToEntitiesEvent(*evt.Event)
			// Some events differ between Libpod and Docker endpoints.
			// Handle these differences for Docker-compat.
			if !utils.IsLibpodRequest(r) && e.Type == "image" && e.Action == "remove" {
				// Status is deprecated, but we still like to set it for consumers that might use it.
				e.Status = "delete" //nolint:staticcheck // deprecated field
				e.Action = "delete"
			}
			if !utils.IsLibpodRequest(r) && e.Action == "died" {
				e.Status = "die" //nolint:staticcheck // deprecated field
				e.Action = "die"
				e.Actor.Attributes["exitCode"] = e.Actor.Attributes["containerExitCode"]
			}
			if !utils.IsLibpodRequest(r) {
				if e.Action == "exec_died" {
					e.Actor.Attributes["exitCode"] = e.Actor.Attributes["containerExitCode"]
				}
				// Docker has no pods and reports exit codes only as "exitCode"
				// on die and exec_die events.
				delete(e.Actor.Attributes, "podId")
				delete(e.Actor.Attributes, "containerExitCode")
				switch evt.Event.Type {
				case events.Network:
					toDockerNetworkEvent(evt.Event, e)
				case events.Volume:
					if !toDockerVolumeEvent(evt.Event, e) {
						continue
					}
				}
			}

			// Remove fields which are not set in 1.52 and newer.
			if _, err := apiutil.SupportedVersion(r, ">=1.52.0"); err == nil && !apiutil.IsLibpodRequest(r) {
				e.Status = "" //nolint:staticcheck // deprecated field, cleared for API >= 1.52
				e.ID = ""     //nolint:staticcheck // deprecated field, cleared for API >= 1.52
				e.From = ""   //nolint:staticcheck // deprecated field, cleared for API >= 1.52
			}

			if err := coder.Encode(e); err != nil {
				logrus.Errorf("Unable to write json: %q", err)
			}
			flush()
		}
	}
}
