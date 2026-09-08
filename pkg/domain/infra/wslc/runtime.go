//go:build windows

package wslc

import (
	"sync"

	"go.podman.io/podman/v6/pkg/domain/entities"
	"go.podman.io/podman/v6/pkg/specgen"
)

type pendingContainer struct {
	ID   string
	Name string
	Spec *specgen.SpecGenerator
}

type ContainerEngine struct {
	mu      sync.Mutex
	pending map[string]*pendingContainer
}

type ImageEngine struct{}

func NewContainerEngine() (entities.ContainerEngine, error) {
	return &ContainerEngine{
		pending: make(map[string]*pendingContainer),
	}, nil
}

func NewImageEngine() (entities.ImageEngine, error) {
	return &ImageEngine{}, nil
}
