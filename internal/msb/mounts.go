package msb

import (
	"fmt"
	"strings"
)

// StatVirtualization selects MicroSandbox guest stat behavior.
type StatVirtualization string

// Supported MicroSandbox mount policy values.
const (
	StatStrict  StatVirtualization = "strict"
	StatRelaxed StatVirtualization = "relaxed"
	StatOff     StatVirtualization = "off"
)

// HostPermissions selects permissions for files created through a bind mount.
type HostPermissions string

// Supported MicroSandbox mount policy values.
const (
	HostPrivate HostPermissions = "private"
	HostMirror  HostPermissions = "mirror"
)

// MountOwner sets the UID and GID presented by a bind mount.
type MountOwner struct {
	UID uint32
	GID uint32
}

// MountPolicy encodes bind-mount virtualization and ownership options.
type MountPolicy struct {
	StatVirtualization StatVirtualization
	HostPermissions    HostPermissions
	Owner              *MountOwner
}

func mountOptions(m Mount) []string {
	var options []string
	if m.ReadOnly {
		options = append(options, "ro")
	}
	if m.Policy.StatVirtualization != "" {
		options = append(options, "stat-virt="+string(m.Policy.StatVirtualization))
	}
	if m.Policy.HostPermissions != "" {
		options = append(options, "host-perms="+string(m.Policy.HostPermissions))
	}
	if m.Policy.Owner != nil {
		options = append(
			options,
			fmt.Sprintf("uid=%d", m.Policy.Owner.UID),
			fmt.Sprintf("gid=%d", m.Policy.Owner.GID),
		)
	}
	return options
}

func bindMountSpec(m Mount) string {
	spec := m.Source + ":" + m.Target
	if options := mountOptions(m); len(options) > 0 {
		spec += ":" + strings.Join(options, ",")
	}
	return spec
}
