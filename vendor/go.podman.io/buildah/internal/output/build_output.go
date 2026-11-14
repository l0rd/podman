package output

import (
	"fmt"
	"strconv"
	"strings"
)

type BuildOutputType int

const (
	BuildOutputInvalid       BuildOutputType = 0
	BuildOutputStdout        BuildOutputType = 1 // stream tar to stdout
	BuildOutputLocalDir      BuildOutputType = 2
	BuildOutputTar           BuildOutputType = 3
	BuildOutputImage         BuildOutputType = 4 // the image written to local storage, as without --output
	BuildOutputRegistry      BuildOutputType = 5 // the image written to local storage and pushed to a registry
	BuildOutputDockerArchive BuildOutputType = 6 // the image written to local storage and saved as a docker-archive
	BuildOutputOCIArchive    BuildOutputType = 7 // the image written to local storage and saved as an oci-archive
	BuildOutputCacheOnly     BuildOutputType = 8 // the image written to local storage without a name, only as build cache
)

func (b BuildOutputType) String() string {
	switch b {
	case BuildOutputInvalid:
		return "invalid (unset)!"
	case BuildOutputStdout:
		return "stdout"
	case BuildOutputLocalDir:
		return "local"
	case BuildOutputTar:
		return "tar"
	case BuildOutputImage:
		return "image"
	case BuildOutputRegistry:
		return "registry"
	case BuildOutputDockerArchive:
		return "docker-archive"
	case BuildOutputOCIArchive:
		return "oci-archive"
	case BuildOutputCacheOnly:
		return "cacheonly"
	}
	return fmt.Sprintf("unknown %d!", int(b))
}

// BuildOutputOptions contains the outcome of parsing the value of a build --output flag
type BuildOutputOption struct {
	Type BuildOutputType
	Path string // Only valid if Type is local dir, tar, docker archive or OCI archive
	// Name is an additional name for the image, with the same meaning as a
	// --tag value. Only valid if Type is image, registry, docker archive or
	// OCI archive; empty if unset.
	Name string
}

// GetBuildOutput is responsible for parsing custom build output argument i.e `build --output` flag.
// Takes `buildOutput` as string and returns BuildOutputOption
func GetBuildOutput(buildOutput string) (BuildOutputOption, error) {
	// Support simple values, in the form --output ./mydir
	if !strings.Contains(buildOutput, ",") && !strings.Contains(buildOutput, "=") {
		if buildOutput == "-" {
			// Feature parity with buildkit, output tar to stdout
			// Read more here: https://docs.docker.com/engine/reference/commandline/build/#custom-build-outputs
			return BuildOutputOption{
				Type: BuildOutputStdout,
				Path: "",
			}, nil
		}

		return BuildOutputOption{
			Type: BuildOutputLocalDir,
			Path: buildOutput,
		}, nil
	}

	// Support complex values, in the form --output type=local,dest=./mydir
	typeValue, typeFound := "", false
	pathSelected := ""
	name := ""
	push, pushFound := false, false
	for option := range strings.SplitSeq(buildOutput, ",") {
		key, value, found := strings.Cut(option, "=")
		if !found {
			return BuildOutputOption{}, fmt.Errorf("invalid build output options %q, expected format key=value", buildOutput)
		}
		switch key {
		case "type":
			if typeFound {
				return BuildOutputOption{}, fmt.Errorf("duplicate %q not supported", key)
			}
			typeValue, typeFound = value, true
		case "dest":
			if pathSelected != "" {
				return BuildOutputOption{}, fmt.Errorf("duplicate %q not supported", key)
			}
			pathSelected = value
		case "name":
			if name != "" {
				return BuildOutputOption{}, fmt.Errorf("duplicate %q not supported", key)
			}
			name = value
		case "push":
			if pushFound {
				return BuildOutputOption{}, fmt.Errorf("duplicate %q not supported", key)
			}
			var err error
			if push, err = strconv.ParseBool(value); err != nil {
				return BuildOutputOption{}, fmt.Errorf("invalid value %q for key %q in build output option %q: %w", value, key, buildOutput, err)
			}
			pushFound = true
		default:
			return BuildOutputOption{}, fmt.Errorf("unrecognized key %q in build output option: %q", key, buildOutput)
		}
	}

	// Validate there is a type
	if !typeFound {
		return BuildOutputOption{}, fmt.Errorf("missing required key %q in build output option: %q", "type", buildOutput)
	}
	var typeSelected BuildOutputType
	switch typeValue {
	case "local":
		typeSelected = BuildOutputLocalDir
	case "tar":
		typeSelected = BuildOutputTar
	case "image":
		typeSelected = BuildOutputImage
		if push {
			typeSelected = BuildOutputRegistry
		}
	case "registry":
		typeSelected = BuildOutputRegistry
	case "docker":
		// Without a destination, docker loads the image into its image
		// store, which for us is the local storage.
		typeSelected = BuildOutputImage
		if pathSelected != "" {
			typeSelected = BuildOutputDockerArchive
		}
	case "oci":
		typeSelected = BuildOutputOCIArchive
	case "cacheonly":
		typeSelected = BuildOutputCacheOnly
	default:
		return BuildOutputOption{}, fmt.Errorf("invalid type %q selected for build output options %q", typeValue, buildOutput)
	}

	// Validate the keys that only apply to some types
	if name != "" && (typeSelected == BuildOutputLocalDir || typeSelected == BuildOutputTar || typeSelected == BuildOutputCacheOnly) {
		return BuildOutputOption{}, fmt.Errorf("key %q is not supported with type %q in build output option: %q", "name", typeValue, buildOutput)
	}
	if pushFound && typeValue != "image" {
		return BuildOutputOption{}, fmt.Errorf("key %q is not supported with type %q in build output option: %q", "push", typeValue, buildOutput)
	}

	// Validate path
	if typeSelected == BuildOutputLocalDir || typeSelected == BuildOutputTar || typeSelected == BuildOutputDockerArchive || typeSelected == BuildOutputOCIArchive {
		if pathSelected == "" {
			return BuildOutputOption{}, fmt.Errorf("missing required key %q in build output option: %q", "dest", buildOutput)
		}
	} else {
		// Clear path when not needed by type
		pathSelected = ""
	}

	// Handle redirecting stdout for tar output
	if pathSelected == "-" {
		if typeSelected == BuildOutputTar {
			typeSelected = BuildOutputStdout
			pathSelected = ""
		} else {
			return BuildOutputOption{}, fmt.Errorf(`invalid build output option %q, only "type=tar" can be used with "dest=-"`, buildOutput)
		}
	}

	return BuildOutputOption{
		Type: typeSelected,
		Path: pathSelected,
		Name: name,
	}, nil
}
