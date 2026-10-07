package msb

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type inspectedConfig struct {
	Labels map[string]string `json:"labels"`
	Mounts []inspectedMount  `json:"mounts"`
}

type inspectedMount struct {
	Type  string `json:"type"`
	Host  string `json:"host"`
	Guest string `json:"guest"`
}

func parseInfo(data []byte) (*Info, error) {
	var raw struct {
		Name         string           `json:"name"`
		Status       string           `json:"status"`
		CreatedAt    string           `json:"created_at"`
		ActiveConfig *inspectedConfig `json:"active_config"`
		Config       *inspectedConfig `json:"config"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse microsandbox inspect output: %w", err)
	}
	current := raw.ActiveConfig
	if current == nil {
		current = raw.Config
	}
	created, _ := time.Parse(time.RFC3339Nano, raw.CreatedAt)
	info := &Info{
		Name:      raw.Name,
		Running:   strings.EqualFold(raw.Status, "running"),
		CreatedAt: created,
	}
	if current != nil {
		info.Labels = current.Labels
		info.Mounts = inspectedMounts(current.Mounts)
	}
	return info, nil
}

func inspectedMounts(raw []inspectedMount) []Mount {
	var mounts []Mount
	for _, mount := range raw {
		switch mount.Type {
		case "Bind":
			mounts = append(mounts, Mount{Source: mount.Host, Target: mount.Guest})
		case "Tmpfs":
			mounts = append(mounts, Mount{Target: mount.Guest, Tmpfs: true})
		}
	}
	return mounts
}
