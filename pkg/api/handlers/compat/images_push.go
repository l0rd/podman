//go:build !remote && (linux || freebsd)

package compat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/moby/moby/api/types/jsonstream"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libimage"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/types"
	"go.podman.io/podman/v6/libpod"
	handlers "go.podman.io/podman/v6/pkg/api/handlers"
	"go.podman.io/podman/v6/pkg/api/handlers/utils"
	"go.podman.io/podman/v6/pkg/api/handlers/utils/apiutil"
	api "go.podman.io/podman/v6/pkg/api/types"
	"go.podman.io/podman/v6/pkg/auth"
	"go.podman.io/podman/v6/pkg/domain/entities"
	"go.podman.io/podman/v6/pkg/domain/infra/abi"
	"go.podman.io/storage"
)

// PushImage is the handler for the compat http endpoint for pushing images.
func PushImage(w http.ResponseWriter, r *http.Request) {
	decoder := utils.GetDecoder(r)
	runtime := r.Context().Value(api.RuntimeKey).(*libpod.Runtime)

	// Now use the ABI implementation to prevent us from having duplicate
	// code.
	imageEngine := abi.ImageEngine{Libpod: runtime}

	query := struct {
		All         bool   `schema:"all"`
		Compress    bool   `schema:"compress"`
		Destination string `schema:"destination"`
		Format      string `schema:"format"`
		TLSVerify   bool   `schema:"tlsVerify"`
		Tag         string `schema:"tag"`
		Platform    string `schema:"platform"`
	}{
		// This is where you can override the golang default value for one of fields
		TLSVerify: true,
	}

	if err := decoder.Decode(&query, r.URL.Query()); err != nil {
		utils.Error(w, http.StatusBadRequest, fmt.Errorf("failed to parse parameters for %s: %w", r.URL.String(), err))
		return
	}

	var platformOS, platformArch, platformVariant string
	if query.Platform != "" {
		var platform struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
			Variant      string `json:"variant,omitempty"`
		}
		if err := json.Unmarshal([]byte(query.Platform), &platform); err != nil {
			utils.Error(w, http.StatusBadRequest, fmt.Errorf("invalid platform JSON: %w", err))
			return
		}
		platformOS = platform.OS
		platformArch = platform.Architecture
		platformVariant = platform.Variant
	}

	// Note that Docker's docs state "Image name or ID" to be in the path
	// parameter but it really must be a name as Docker does not allow for
	// pushing an image by ID.
	pathName := strings.TrimSuffix(utils.GetName(r), "/push") // GetName returns the entire path
	imageName := pathName
	if query.Tag != "" {
		imageName += ":" + query.Tag
	}

	if _, err := utils.ParseStorageReference(imageName); err != nil {
		utils.Error(w, http.StatusBadRequest, fmt.Errorf("image source %q is not a containers-storage-transport reference: %w", imageName, err))
		return
	}

	possiblyNormalizedName, err := utils.NormalizeToDockerHub(r, imageName)
	if err != nil {
		utils.Error(w, http.StatusInternalServerError, fmt.Errorf("normalizing image: %w", err))
		return
	}
	imageName = possiblyNormalizedName

	type pushItem struct {
		name         string
		tag          string
		manifestSize int
	}
	tag := query.Tag
	if tag == "" {
		tag = "latest"
	}
	items := []pushItem{{name: imageName, tag: tag}}
	repoName := imageName
	_, destinationFound := r.URL.Query()["destination"]
	if query.Tag == "" && !destinationFound {
		// Docker pushes all local tags of the repository when neither the
		// path nor the query has a tag.
		name, tags, err := localRepositoryTags(r.Context(), runtime, pathName)
		if err != nil {
			utils.InternalServerError(w, fmt.Errorf("listing local tags of %s: %w", pathName, err))
			return
		}
		if len(tags) > 0 {
			repoName = name
			items = items[:0]
			for _, t := range tags {
				items = append(items, pushItem{name: name + ":" + t, tag: t})
			}
		}
	}

	lookupOptions := &libimage.LookupImageOptions{
		OS:           platformOS,
		Architecture: platformArch,
		Variant:      platformVariant,
	}
	for i := range items {
		localImage, _, err := runtime.LibimageRuntime().LookupImage(items[i].name, lookupOptions)
		if err != nil {
			if errors.Is(err, storage.ErrImageUnknown) {
				utils.Error(w, http.StatusNotFound, imageNotExistLocallyError(items[i].name))
				return
			}
			utils.InternalServerError(w, fmt.Errorf("failed to find image %s: %w", items[i].name, err))
			return
		}
		rawManifest, _, err := localImage.Manifest(r.Context())
		if err != nil {
			utils.Error(w, http.StatusBadRequest, err)
			return
		}
		items[i].manifestSize = len(rawManifest)
	}

	authconf, authfile, err := auth.GetCredentials(r)
	if err != nil {
		utils.Error(w, http.StatusBadRequest, err)
		return
	}
	defer auth.RemoveAuthfile(authfile)
	var username, password string
	if authconf != nil {
		username = authconf.Username
		password = authconf.Password
	}
	options := entities.ImagePushOptions{
		All:      query.All,
		Authfile: authfile,
		Compress: query.Compress,
		Format:   query.Format,
		Password: password,
		Username: username,
		Quiet:    true,
		Progress: make(chan types.ProgressProperties),
		OS:       platformOS,
		Arch:     platformArch,
		Variant:  platformVariant,
	}
	if _, found := r.URL.Query()["tlsVerify"]; found {
		options.SkipTLSVerify = types.NewOptionalBool(!query.TLSVerify)
	}

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)

	flush := func() {}
	if flusher, ok := w.(http.Flusher); ok {
		flush = flusher.Flush
	}

	statusWritten := false
	writeStatusCode := func(code int) {
		if !statusWritten {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			flush()
			statusWritten = true
		}
	}

	referenceWritten := false
	writeReference := func() {
		if !referenceWritten {
			var report jsonstream.Message
			report.Status = fmt.Sprintf("The push refers to repository [%s]", repoName)
			if err := enc.Encode(report); err != nil {
				logrus.Warnf("Failed to json encode error %q", err.Error())
			}
			flush()
			referenceWritten = true
		}
	}

	type pushResult struct {
		item   pushItem
		report *entities.ImagePushReport
		err    error
	}
	pushResults := make(chan pushResult)
	go func() {
		for _, item := range items {
			destination := item.name
			if destinationFound {
				destination = query.Destination
			}
			report, err := imageEngine.Push(r.Context(), item.name, destination, options)
			pushResults <- pushResult{item: item, report: report, err: err}
			if err != nil {
				return
			}
		}
	}()
	pushed := 0

loop: // break out of for/select infinite loop
	for {
		var report handlers.LegacyJSONMessage //nolint:staticcheck // LegacyJSONMessage is deprecated but kept for Docker API compat < v1.52

		select {
		case e := <-options.Progress:
			writeStatusCode(http.StatusOK)
			writeReference()
			switch e.Event {
			case types.ProgressEventNewArtifact:
				report.Status = "Preparing"
			case types.ProgressEventRead:
				report.Status = "Pushing"
				report.Progress = &jsonstream.Progress{
					Current: int64(e.Offset),
					Total:   e.Artifact.Size,
				}
				if _, err := apiutil.SupportedVersion(r, "<1.52.0"); err == nil {
					b, err := json.Marshal(&jsonstream.Message{
						Status:   report.Status,
						Progress: report.Progress,
						ID:       report.ID,
					})
					if err == nil {
						report.ProgressMessage = string(b)
					}
				}
			case types.ProgressEventSkipped:
				report.Status = "Layer already exists"
			case types.ProgressEventDone:
				report.Status = "Pushed"
			}
			report.ID = e.Artifact.Digest.Encoded()[0:12]
			if err := enc.Encode(report); err != nil {
				logrus.Warnf("Failed to json encode error %q", err.Error())
			}
			flush()
		case res := <-pushResults:
			if err := res.err; err != nil {
				code := http.StatusInternalServerError
				if errors.Is(err, storage.ErrImageUnknown) {
					// Image may have been removed in the meantime.
					code = http.StatusNotFound
					err = imageNotExistLocallyError(res.item.name)
				}
				if !statusWritten {
					utils.Error(w, code, err)
					flush()
					break loop
				}
				msg := err.Error()
				report.Error = &jsonstream.Error{
					Message: msg,
				}
				if _, err := apiutil.SupportedVersion(r, "<1.52.0"); err == nil {
					report.ErrorMessage = msg //nolint:staticcheck // deprecated field
				}
				if err := enc.Encode(report); err != nil {
					logrus.Warnf("Failed to json encode error %q", err.Error())
				}
				flush()
				break loop
			} else {
				writeStatusCode(http.StatusOK)
				writeReference() // There may not be any progress, so make sure the reference gets written
			}

			var digestStr string
			if res.report != nil {
				digestStr = res.report.ManifestDigest
			}
			// Match Docker's behaviour: emit the manifest digest as an
			// `aux` JSON trailer at the end of the push stream so clients
			// can recover it without round-tripping the registry.
			auxPayload, auxErr := json.Marshal(struct {
				Tag    string `json:"Tag"`
				Digest string `json:"Digest"`
				Size   int    `json:"Size"`
			}{
				Tag:    res.item.tag,
				Digest: digestStr,
				Size:   res.item.manifestSize,
			})
			if auxErr == nil {
				raw := json.RawMessage(auxPayload)
				report.Aux = &raw
			}
			report.Status = fmt.Sprintf("%s: digest: %s size: %d", res.item.tag, digestStr, res.item.manifestSize)
			if err := enc.Encode(report); err != nil {
				logrus.Warnf("Failed to json encode error %q", err.Error())
			}

			flush()
			pushed++
			if pushed == len(items) {
				break loop // break out of for/select infinite loop
			}
		}
	}
}

// localRepositoryTags returns the normalized repository of name and the
// sorted tags of its local images.  It returns no tags if name is not a bare
// repository name, i.e. it has a tag or a digest.
func localRepositoryTags(ctx context.Context, runtime *libpod.Runtime, name string) (string, []string, error) {
	named, err := reference.ParseNormalizedNamed(name)
	if err != nil || !reference.IsNameOnly(named) {
		return "", nil, nil //nolint:nilerr // the caller reports invalid names
	}
	images, err := runtime.LibimageRuntime().ListImages(ctx, &libimage.ListImagesOptions{})
	if err != nil {
		return "", nil, err
	}
	var tags []string
	for _, img := range images {
		repoTags, err := img.NamedTaggedRepoTags()
		if err != nil {
			return "", nil, err
		}
		for _, repoTag := range repoTags {
			if repoTag.Name() == named.Name() {
				tags = append(tags, repoTag.Tag())
			}
		}
	}
	slices.Sort(tags)
	return named.Name(), tags, nil
}

// imageNotExistLocallyError returns the error Docker reports when the image to
// push is not in the local store. Clients match on this text.
func imageNotExistLocallyError(name string) error {
	if named, err := reference.ParseNormalizedNamed(name); err == nil {
		name = reference.FamiliarName(named)
	}
	return errors.New("An image does not exist locally with the tag: " + name)
}
