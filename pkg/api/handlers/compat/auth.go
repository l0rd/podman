//go:build !remote && (linux || freebsd)

package compat

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/moby/moby/api/types/registry"
	"go.podman.io/common/pkg/auth"
	DockerClient "go.podman.io/image/v5/docker"
	"go.podman.io/image/v5/types"
	"go.podman.io/podman/v6/libpod"
	"go.podman.io/podman/v6/pkg/api/handlers/utils"
	api "go.podman.io/podman/v6/pkg/api/types"
	"go.podman.io/podman/v6/pkg/domain/entities"
)

func Auth(w http.ResponseWriter, r *http.Request) {
	var authConfig registry.AuthConfig
	if err := utils.ReadJSONFromBody(r, &authConfig); err != nil {
		utils.Error(w, http.StatusBadRequest, err)
		return
	}

	runtime := r.Context().Value(api.RuntimeKey).(*libpod.Runtime)
	sysCtx := runtime.SystemContext()
	// TLS verification is configured in registries.conf, but localhost
	// registries skip it to support local testing.
	if isLocalhostServerAddress(authConfig.ServerAddress) {
		sysCtx.DockerInsecureSkipTLSVerify = types.OptionalBoolTrue
	}

	loginOpts := &auth.LoginOptions{
		Username:    authConfig.Username,
		Password:    authConfig.Password,
		Stdout:      io.Discard,
		NoWriteBack: true, // to prevent credentials to be written on disk
	}
	if err := auth.Login(r.Context(), sysCtx, loginOpts, []string{authConfig.ServerAddress}); err == nil {
		utils.WriteResponse(w, http.StatusOK, entities.AuthReport{
			IdentityToken: "",
			Status:        "Login Succeeded",
		})
	} else {
		var msg string

		if _, ok := errors.AsType[DockerClient.ErrUnauthorizedForCredentials](err); ok {
			msg = "401 Unauthorized"
		} else {
			msg = err.Error()
		}

		utils.WriteResponse(w, http.StatusInternalServerError, struct {
			Message string `json:"message"`
		}{
			Message: "login attempt to " + authConfig.ServerAddress + " failed with status: " + msg,
		})
	}
}

// isLocalhostServerAddress reports whether serverAddress, a bare host[:port]
// or a URL such as "https://localhost:5000/v2/", names a registry on localhost.
func isLocalhostServerAddress(serverAddress string) bool {
	if !strings.Contains(serverAddress, "://") {
		serverAddress = "//" + serverAddress
	}
	u, err := url.Parse(serverAddress)
	if err != nil {
		return false
	}
	return u.Hostname() == "localhost"
}
