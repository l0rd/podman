//go:build windows

package wslc

import (
	"context"
	"fmt"
	"io"

	"go.podman.io/common/libimage/define"
	"go.podman.io/common/pkg/config"
	"go.podman.io/podman/v6/pkg/domain/entities"
	"go.podman.io/podman/v6/pkg/domain/entities/reports"
)

var errImageNotSupported = fmt.Errorf("not supported by wslc image engine")

func (e *ImageEngine) List(ctx context.Context, opts entities.ImageListOptions) ([]*entities.ImageSummary, error) {
	out, err := runWslc("image", "list")
	if err != nil {
		return nil, err
	}
	return parseImageList(out)
}

func (e *ImageEngine) Shutdown(ctx context.Context) {}

func (e *ImageEngine) ArtifactAdd(ctx context.Context, name string, artifactBlobs []entities.ArtifactBlob, opts entities.ArtifactAddOptions) (*entities.ArtifactAddReport, error) {
	return nil, errImageNotSupported
}

func (e *ImageEngine) ArtifactExtract(ctx context.Context, name string, target string, opts entities.ArtifactExtractOptions) error {
	return errImageNotSupported
}

func (e *ImageEngine) ArtifactExtractTarStream(ctx context.Context, w io.Writer, name string, opts entities.ArtifactExtractOptions) error {
	return errImageNotSupported
}

func (e *ImageEngine) ArtifactInspect(ctx context.Context, name string, opts entities.ArtifactInspectOptions) (*entities.ArtifactInspectReport, error) {
	return nil, errImageNotSupported
}

func (e *ImageEngine) ArtifactList(ctx context.Context, opts entities.ArtifactListOptions) ([]*entities.ArtifactListReport, error) {
	return nil, errImageNotSupported
}

func (e *ImageEngine) ArtifactPull(ctx context.Context, name string, opts entities.ArtifactPullOptions) (*entities.ArtifactPullReport, error) {
	return nil, errImageNotSupported
}

func (e *ImageEngine) ArtifactPush(ctx context.Context, name string, opts entities.ArtifactPushOptions) (*entities.ArtifactPushReport, error) {
	return nil, errImageNotSupported
}

func (e *ImageEngine) ArtifactRm(ctx context.Context, opts entities.ArtifactRemoveOptions) (*entities.ArtifactRemoveReport, error) {
	return nil, errImageNotSupported
}

func (e *ImageEngine) Build(ctx context.Context, containerFiles []string, opts entities.BuildOptions) (*entities.BuildReport, error) {
	return nil, errImageNotSupported
}

func (e *ImageEngine) Config(ctx context.Context) (*config.Config, error) {
	return nil, errImageNotSupported
}

func (e *ImageEngine) Exists(ctx context.Context, nameOrID string) (*entities.BoolReport, error) {
	return &entities.BoolReport{Value: true}, nil
}

func (e *ImageEngine) History(ctx context.Context, nameOrID string, opts entities.ImageHistoryOptions) (*entities.ImageHistoryReport, error) {
	return nil, errImageNotSupported
}

func (e *ImageEngine) Import(ctx context.Context, opts entities.ImageImportOptions) (*entities.ImageImportReport, error) {
	return nil, errImageNotSupported
}

func (e *ImageEngine) Inspect(ctx context.Context, namesOrIDs []string, opts entities.InspectOptions) ([]*entities.ImageInspectReport, []error, error) {
	return nil, nil, errImageNotSupported
}

func (e *ImageEngine) Load(ctx context.Context, opts entities.ImageLoadOptions) (*entities.ImageLoadReport, error) {
	return nil, errImageNotSupported
}

func (e *ImageEngine) Mount(ctx context.Context, images []string, options entities.ImageMountOptions) ([]*entities.ImageMountReport, error) {
	return nil, errImageNotSupported
}

func (e *ImageEngine) Prune(ctx context.Context, opts entities.ImagePruneOptions) ([]*reports.PruneReport, error) {
	return nil, errImageNotSupported
}

// Pull is a no-op because wslc auto-pulls images on `wslc run`.
func (e *ImageEngine) Pull(ctx context.Context, rawImage string, opts entities.ImagePullOptions) (*entities.ImagePullReport, error) {
	return &entities.ImagePullReport{
		Images: []string{rawImage},
	}, nil
}

func (e *ImageEngine) Push(ctx context.Context, source string, destination string, opts entities.ImagePushOptions) (*entities.ImagePushReport, error) {
	return nil, errImageNotSupported
}

func (e *ImageEngine) Remove(ctx context.Context, images []string, opts entities.ImageRemoveOptions) (*entities.ImageRemoveReport, []error) {
	return nil, []error{errImageNotSupported}
}

func (e *ImageEngine) Save(ctx context.Context, nameOrID string, tags []string, options entities.ImageSaveOptions) error {
	return errImageNotSupported
}

func (e *ImageEngine) Scp(ctx context.Context, src, dst string, opts entities.ImageScpOptions) (*entities.ImageScpReport, error) {
	return nil, errImageNotSupported
}

func (e *ImageEngine) Search(ctx context.Context, term string, opts entities.ImageSearchOptions) ([]entities.ImageSearchReport, error) {
	return nil, errImageNotSupported
}

func (e *ImageEngine) SetTrust(ctx context.Context, args []string, options entities.SetTrustOptions) error {
	return errImageNotSupported
}

func (e *ImageEngine) ShowTrust(ctx context.Context, args []string, options entities.ShowTrustOptions) (*entities.ShowTrustReport, error) {
	return nil, errImageNotSupported
}

func (e *ImageEngine) Tag(ctx context.Context, nameOrID string, tags []string, options entities.ImageTagOptions) error {
	return errImageNotSupported
}

func (e *ImageEngine) Tree(ctx context.Context, nameOrID string, options entities.ImageTreeOptions) (*entities.ImageTreeReport, error) {
	return nil, errImageNotSupported
}

func (e *ImageEngine) Unmount(ctx context.Context, images []string, options entities.ImageUnmountOptions) ([]*entities.ImageUnmountReport, error) {
	return nil, errImageNotSupported
}

func (e *ImageEngine) Untag(ctx context.Context, nameOrID string, tags []string, options entities.ImageUntagOptions) error {
	return errImageNotSupported
}

func (e *ImageEngine) ManifestCreate(ctx context.Context, name string, images []string, opts entities.ManifestCreateOptions) (string, error) {
	return "", errImageNotSupported
}

func (e *ImageEngine) ManifestExists(ctx context.Context, name string) (*entities.BoolReport, error) {
	return nil, errImageNotSupported
}

func (e *ImageEngine) ManifestInspect(ctx context.Context, name string, opts entities.ManifestInspectOptions) (*define.ManifestListData, error) {
	return nil, errImageNotSupported
}

func (e *ImageEngine) ManifestAdd(ctx context.Context, listName string, imageNames []string, opts entities.ManifestAddOptions) (string, error) {
	return "", errImageNotSupported
}

func (e *ImageEngine) ManifestAddArtifact(ctx context.Context, name string, files []string, opts entities.ManifestAddArtifactOptions) (string, error) {
	return "", errImageNotSupported
}

func (e *ImageEngine) ManifestAnnotate(ctx context.Context, names, image string, opts entities.ManifestAnnotateOptions) (string, error) {
	return "", errImageNotSupported
}

func (e *ImageEngine) ManifestRemoveDigest(ctx context.Context, names, image string) (string, error) {
	return "", errImageNotSupported
}

func (e *ImageEngine) ManifestRm(ctx context.Context, names []string, imageRmOpts entities.ImageRemoveOptions) (*entities.ImageRemoveReport, []error) {
	return nil, []error{errImageNotSupported}
}

func (e *ImageEngine) ManifestPush(ctx context.Context, name, destination string, imagePushOpts entities.ImagePushOptions) (string, error) {
	return "", errImageNotSupported
}

func (e *ImageEngine) ManifestListClear(ctx context.Context, name string) (string, error) {
	return "", errImageNotSupported
}

func (e *ImageEngine) Sign(ctx context.Context, names []string, options entities.SignOptions) (*entities.SignReport, error) {
	return nil, errImageNotSupported
}

func (e *ImageEngine) FarmNodeName(ctx context.Context) string {
	return ""
}

func (e *ImageEngine) FarmNodeDriver(ctx context.Context) string {
	return ""
}

func (e *ImageEngine) FarmNodeInspect(ctx context.Context) (*entities.FarmInspectReport, error) {
	return nil, errImageNotSupported
}
