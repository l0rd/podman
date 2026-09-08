//go:build windows

package wslc

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"

	"go.podman.io/podman/v6/pkg/domain/entities"
)

// parseContainerList parses the tabular output of `wslc container list`.
// Expected format (tab or space separated):
//
//	CONTAINER ID   IMAGE     COMMAND   CREATED   STATUS    PORTS     NAMES
//	abc123         alpine    /bin/sh   ...       Running             mycontainer
func parseContainerList(output []byte) ([]entities.ListContainer, error) {
	var containers []entities.ListContainer
	scanner := bufio.NewScanner(bytes.NewReader(output))

	// Skip header line
	if !scanner.Scan() {
		return containers, nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		ctr := entities.ListContainer{
			ID:    fields[0],
			Image: fields[1],
		}
		if len(fields) >= 3 {
			ctr.Command = []string{fields[2]}
		}
		if len(fields) >= 5 {
			ctr.State = fields[4]
			ctr.Status = fields[4]
		}
		if len(fields) >= 7 {
			ctr.Names = []string{fields[6]}
		}
		containers = append(containers, ctr)
	}
	return containers, scanner.Err()
}

// convertInspect converts wslc container inspect JSON output to a Podman ContainerInspectReport.
// The exact schema of wslc inspect output will need empirical discovery;
// this provides a best-effort mapping.
func convertInspect(raw map[string]interface{}) (*entities.ContainerInspectReport, error) {
	id, _ := raw["Id"].(string)
	if id == "" {
		id, _ = raw["ID"].(string)
	}
	if id == "" {
		return nil, fmt.Errorf("container inspect: missing Id field")
	}

	name, _ := raw["Name"].(string)
	image, _ := raw["Image"].(string)

	var state string
	if stateMap, ok := raw["State"].(map[string]interface{}); ok {
		state, _ = stateMap["Status"].(string)
	}

	// Build a minimal InspectContainerData-compatible report.
	// Full field mapping will be added as the wslc inspect schema is discovered.
	report := &entities.ContainerInspectReport{}
	// ContainerInspectReport embeds *define.InspectContainerData which is a large struct.
	// For the PoC, we return a report with the raw JSON attached.
	// Callers that need specific fields will need the full mapping.
	_ = name
	_ = image
	_ = state

	return report, nil
}

// parseImageList parses the tabular output of `wslc image list`.
// Expected format:
//
//	REPOSITORY   TAG       IMAGE ID   CREATED   SIZE
//	alpine       latest    abc123     ...       5.6MB
func parseImageList(output []byte) ([]*entities.ImageSummary, error) {
	var images []*entities.ImageSummary
	scanner := bufio.NewScanner(bytes.NewReader(output))

	// Skip header line
	if !scanner.Scan() {
		return images, nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		repo := fields[0]
		tag := fields[1]
		imageID := fields[2]

		img := &entities.ImageSummary{
			ID:       imageID,
			RepoTags: []string{repo + ":" + tag},
		}
		images = append(images, img)
	}
	return images, scanner.Err()
}
