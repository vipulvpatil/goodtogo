package main

import (
	"encoding/json"
	"os/exec"
	"strings"
)

// canaryBuildTag returns the commit-ts tag applied to whatfpl:canary at build
// time (e.g. "a1b2c3d4-20260302-153045"). Returns "unknown" if not found.
func canaryBuildTag() string {
	out, err := exec.Command("docker", "image", "inspect", "whatfpl:canary",
		"--format", "{{json .RepoTags}}").Output()
	if err != nil {
		return "unknown"
	}
	var tags []string
	if err := json.Unmarshal(out, &tags); err != nil {
		return "unknown"
	}
	for _, t := range tags {
		tag, ok := strings.CutPrefix(t, "whatfpl:")
		if ok && tag != "canary" && tag != "baseline" {
			return tag
		}
	}
	return "unknown"
}
