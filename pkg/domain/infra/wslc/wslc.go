//go:build windows

package wslc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
)

var wslcBinary = "wslc"

var execCommand = exec.Command

func runWslc(args ...string) ([]byte, error) {
	cmd := execCommand(wslcBinary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("wslc %v: %s: %w", args, stderr.String(), err)
	}
	return stdout.Bytes(), nil
}

func runWslcJSON(result interface{}, args ...string) error {
	out, err := runWslc(args...)
	if err != nil {
		return err
	}
	return json.Unmarshal(out, result)
}
