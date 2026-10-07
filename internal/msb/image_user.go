package msb

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

const (
	maxAccountFileSize = 4 << 20
	rootUser           = "root"
	passwdPath         = "etc/passwd"
	groupPath          = "etc/group"
)

type imageUserIdentity struct {
	name, group            string
	uid, gid               uint32
	numericUID, numericGID bool
}

// WorkspaceOwnerOptions separates the workload user from the developer identity.
// Only the primary workspace bind mount receives a resolved owner.
type WorkspaceOwnerOptions struct {
	User           string
	RemoteUser     string
	Dockerless     bool
	WorkspaceMount *Mount
}

// ResolveWorkspaceOwner reads account metadata from the snapshot to be imported.
// Call it before importing images or changing an existing VM, and keep the snapshot
// open until all image reads finish. It never executes image code or changes host ownership.
func ResolveWorkspaceOwner(
	ctx context.Context,
	prepared *PreparedImage,
	options WorkspaceOwnerOptions,
) (*MountOwner, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !needsWorkspaceOwner(options.WorkspaceMount) {
		return nil, nil
	}
	user := workspaceRemoteUser(options)
	if options.Dockerless {
		if user != rootUser {
			return nil, errors.New(
				"microsandbox cannot resolve a non-root Dockerless workspace owner before VM creation; " +
					"use a prebuilt developer image, remoteUser=root, or " +
					"workspaceStatVirtualization=off with private host permissions",
			)
		}
		return &MountOwner{}, nil
	}
	owner, err := resolvePreparedOwner(ctx, prepared, user)
	if err != nil {
		return nil, fmt.Errorf(
			"resolve microsandbox workspace owner for remoteUser %q: %w",
			user,
			err,
		)
	}
	return owner, nil
}

func needsWorkspaceOwner(mount *Mount) bool {
	return mount != nil && mount.Source != "" && mount.Target != "" &&
		mount.Volume == "" && !mount.Tmpfs && mount.Policy.StatVirtualization != StatOff
}

func workspaceRemoteUser(options WorkspaceOwnerOptions) string {
	if options.RemoteUser != "" {
		return options.RemoteUser
	}
	if options.User != "" {
		return options.User
	}
	return rootUser
}

func resolvePreparedOwner(
	ctx context.Context,
	prepared *PreparedImage,
	user string,
) (*MountOwner, error) {
	var img v1.Image
	if prepared != nil && prepared.reference != "" {
		img = prepared.image
	}
	return ownerFromImage(ctx, img, user)
}

func numericID(value string) (uint32, bool, error) {
	if value == "" {
		return 0, false, nil
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, false, nil
		}
	}
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, true, fmt.Errorf("invalid numeric account ID %q: %w", value, err)
	}
	return uint32(n), true, nil
}

func parseImageUserIdentity(user string) (imageUserIdentity, error) {
	name, group, hasGroup := strings.Cut(user, ":")
	if name == "" || (hasGroup && (group == "" || strings.Contains(group, ":"))) {
		return imageUserIdentity{}, fmt.Errorf("invalid user identity %q", user)
	}
	uid, numericUID, err := numericID(name)
	if err != nil {
		return imageUserIdentity{}, err
	}
	gid, numericGID, err := numericID(group)
	if err != nil {
		return imageUserIdentity{}, err
	}
	return imageUserIdentity{
		name:       name,
		group:      group,
		uid:        uid,
		gid:        gid,
		numericUID: numericUID,
		numericGID: numericGID,
	}, nil
}

func (identity imageUserIdentity) explicitOwner() (*MountOwner, bool) {
	if identity.group == "" {
		if identity.name == rootUser || (identity.numericUID && identity.uid == 0) {
			return &MountOwner{}, true
		}
	}
	if identity.numericUID && identity.numericGID {
		return &MountOwner{UID: identity.uid, GID: identity.gid}, true
	}
	return nil, false
}

func (identity imageUserIdentity) accountFiles() map[string]bool {
	wanted := map[string]bool{}
	if !identity.numericUID || identity.group == "" {
		wanted[passwdPath] = true
	}
	if identity.group != "" && !identity.numericGID {
		wanted[groupPath] = true
	}
	return wanted
}

func ownerFromImage(ctx context.Context, img v1.Image, user string) (*MountOwner, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	identity, err := parseImageUserIdentity(user)
	if err != nil {
		return nil, err
	}
	if owner, complete := identity.explicitOwner(); complete {
		return owner, nil
	}
	if img == nil {
		return nil, errors.New("workspace owner resolution requires a prepared image")
	}
	accounts, err := imageAccountFiles(ctx, img, identity.accountFiles())
	if err != nil {
		return nil, err
	}
	return ownerFromAccounts(user, accounts[passwdPath], accounts[groupPath])
}

func imageAccountFiles(
	ctx context.Context,
	img v1.Image,
	wanted map[string]bool,
) (map[string]string, error) {
	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("read image layers: %w", errors.Join(err, ctx.Err()))
	}
	accounts := map[string]string{}
	hidden := map[string]bool{}
	for i := len(layers) - 1; i >= 0 && len(hidden) < len(wanted); i-- {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		files, removed, err := layerAccountFiles(ctx, layers[i], accountLayer{
			accounts: map[string]string{},
			removed:  map[string]bool{},
			hidden:   hidden,
			wanted:   wanted,
		})
		if err != nil {
			return nil, err
		}
		for filename, data := range files {
			accounts[filename], hidden[filename] = data, true
		}
		// Whiteouts hide lower layers, not replacements in this layer.
		for filename := range removed {
			hidden[filename] = true
		}
	}
	return accounts, ctx.Err()
}

func layerAccountFiles(
	ctx context.Context,
	layer v1.Layer,
	files accountLayer,
) (map[string]string, map[string]bool, error) {
	stream, err := layer.Uncompressed()
	if err != nil {
		return nil, nil, fmt.Errorf("open image layer: %w", errors.Join(err, ctx.Err()))
	}
	defer func() { _ = stream.Close() }()
	stopClose := context.AfterFunc(ctx, func() { _ = stream.Close() })
	defer stopClose()
	reader := tar.NewReader(stream)
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		header, err := reader.Next()
		if err == io.EOF {
			return files.accounts, files.removed, ctx.Err()
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read image accounts: %w", errors.Join(err, ctx.Err()))
		}
		if err := files.collect(reader, header); err != nil {
			return nil, nil, errors.Join(err, ctx.Err())
		}
	}
}

type accountLayer struct {
	accounts map[string]string
	removed  map[string]bool
	hidden   map[string]bool
	wanted   map[string]bool
}

func (files accountLayer) collect(reader io.Reader, header *tar.Header) error {
	filename := path.Clean(strings.TrimPrefix(header.Name, "/"))
	files.hideWhiteout(filename)
	if filename == "etc" && header.Typeflag != tar.TypeDir {
		return errors.New("final image /etc is not a regular account directory")
	}
	if !files.wanted[filename] {
		return nil
	}
	if _, seen := files.accounts[filename]; seen || files.hidden[filename] {
		return nil
	}
	data, err := readAccountFile(reader, header)
	if err != nil {
		return err
	}
	files.accounts[filename] = data
	return nil
}

func (files accountLayer) hideWhiteout(filename string) {
	for _, target := range accountWhiteoutTargets(filename) {
		if files.wanted[target] {
			files.removed[target] = true
		}
	}
}

func accountWhiteoutTargets(filename string) []string {
	switch filename {
	case ".wh.etc", ".wh..wh..opq", "etc/.wh..wh..opq":
		return []string{passwdPath, groupPath}
	case "etc/.wh.passwd":
		return []string{passwdPath}
	case "etc/.wh.group":
		return []string{groupPath}
	default:
		return nil
	}
}

func readAccountFile(reader io.Reader, header *tar.Header) (string, error) {
	if header.Typeflag != tar.TypeReg {
		return "", fmt.Errorf("final image /%s is not a regular account file", header.Name)
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxAccountFileSize+1))
	if err != nil {
		return "", fmt.Errorf("read image /%s: %w", header.Name, err)
	}
	if len(data) > maxAccountFileSize {
		return "", fmt.Errorf("image /%s exceeds account file size limit", header.Name)
	}
	return string(data), nil
}

func ownerFromAccounts(user, passwd, groups string) (*MountOwner, error) {
	name, group, hasGroup := strings.Cut(user, ":")
	uid, numeric, err := numericID(name)
	if err != nil {
		return nil, err
	}
	owner := &MountOwner{UID: uid}
	// Explicit numeric IDs need no passwd entry when a group is supplied.
	if !numeric || !hasGroup {
		owner, err = passwdOwner(passwd, name, uid, numeric)
		if err != nil {
			return nil, err
		}
	}
	if hasGroup {
		owner.GID, err = groupID(groups, group)
		if err != nil {
			return nil, err
		}
	}
	return owner, nil
}

func passwdOwner(passwd, name string, uid uint32, numeric bool) (*MountOwner, error) {
	for line := range strings.SplitSeq(passwd, "\n") {
		fields := strings.Split(line, ":")
		if len(fields) != 7 {
			continue
		}
		if !matchesPasswdRow(fields, name, uid, numeric) {
			continue
		}
		return passwdRowOwner(fields)
	}
	return nil, fmt.Errorf("user %q not found in final image /etc/passwd", name)
}

func matchesPasswdRow(fields []string, name string, uid uint32, numeric bool) bool {
	if !numeric {
		return fields[0] == name
	}
	entryUID, valid, err := numericID(fields[2])
	return valid && err == nil && entryUID == uid
}

func passwdRowOwner(fields []string) (*MountOwner, error) {
	uid, validUID, uidErr := numericID(fields[2])
	gid, validGID, gidErr := numericID(fields[3])
	if uidErr != nil || gidErr != nil || !validUID || !validGID {
		return nil, fmt.Errorf("invalid account IDs for %q in final image /etc/passwd", fields[0])
	}
	return &MountOwner{UID: uid, GID: gid}, nil
}

func groupID(groups, group string) (uint32, error) {
	gid, numeric, err := numericID(group)
	if err != nil || numeric {
		return gid, err
	}
	for line := range strings.SplitSeq(groups, "\n") {
		fields := strings.Split(line, ":")
		if len(fields) != 4 || fields[0] != group {
			continue
		}
		gid, valid, err := numericID(fields[2])
		if err != nil || !valid {
			return 0, fmt.Errorf("invalid group ID for %q in final image /etc/group", group)
		}
		return gid, nil
	}
	return 0, fmt.Errorf("group %q not found in final image /etc/group", group)
}
