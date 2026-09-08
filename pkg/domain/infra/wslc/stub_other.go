//go:build !windows

package wslc

import (
	"fmt"

	"go.podman.io/podman/v6/pkg/domain/entities"
)

func NewContainerEngine() (entities.ContainerEngine, error) {
	return nil, fmt.Errorf("wslc engine is only available on Windows")
}

func NewImageEngine() (entities.ImageEngine, error) {
	return nil, fmt.Errorf("wslc engine is only available on Windows")
}
