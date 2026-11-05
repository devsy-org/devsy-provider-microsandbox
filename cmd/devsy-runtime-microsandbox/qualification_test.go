package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	microsandbox "github.com/superradcompany/microsandbox/sdk/go"
)

const (
	qualifiedArchiveURL = "https://github.com/superradcompany/microsandbox/releases/download/" +
		"v0.7.13/microsandbox-linux-x86_64.tar.gz"
	qualifiedArchiveSHA256 = "c41aeefc050e97f42cd55ce55cc139efc13c6ee2dc371e8d30c325a305a5c3b1"
	qualifiedArchiveBytes  = 30542710
	qualifiedExpandedBytes = 75 << 20
	qualifiedMSBBytes      = 54239160
	qualifiedMSBName       = "msb"
	qualifiedFirmwareBytes = 21628880
	qualifiedFirmwareName  = "libkrunfw.so.5.6.1"
)

func TestRelocatedPinnedRuntime(t *testing.T) {
	if os.Getenv("DEVSY_QUALIFY_RUNTIME") != "1" {
		t.Skip("requires explicit native relocation qualification")
	}
	require.Equal(t, "linux", runtime.GOOS)
	require.Equal(t, "amd64", runtime.GOARCH)
	adapter := os.Getenv("DEVSY_ADAPTER_BINARY")
	require.True(t, filepath.IsAbs(adapter))
	originalHome, err := os.UserHomeDir()
	require.NoError(t, err)
	originalRuntime := filepath.Join(originalHome, ".microsandbox")
	_, err = os.Lstat(originalRuntime)
	require.ErrorIs(t, err, os.ErrNotExist)
	t.Cleanup(func() {
		_, err := os.Lstat(originalRuntime)
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	root := t.TempDir()
	for _, name := range []string{"home", "devsy", qualifiedMSBName, "bundle"} {
		require.NoError(t, os.Mkdir(filepath.Join(root, name), 0o700))
	}
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	t.Setenv("DEVSY_HOME", filepath.Join(root, "devsy"))
	t.Setenv("MSB_HOME", filepath.Join(root, qualifiedMSBName))
	t.Setenv("MSB_CONFIG_PATH", filepath.Join(root, qualifiedMSBName, "config.json"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))

	archive := downloadQualifiedArchive(t, root)
	msbPath, firmwarePath := extractQualifiedPair(t, archive, filepath.Join(root, "bundle"))
	verifyQualifiedRuntime(t, qualifiedRuntimePaths{
		adapter: adapter, home: filepath.Join(root, qualifiedMSBName),
		msb: msbPath, firmware: firmwarePath,
	})
}

type qualifiedRuntimePaths struct {
	adapter  string
	home     string
	msb      string
	firmware string
}

func verifyQualifiedRuntime(t *testing.T, paths qualifiedRuntimePaths) {
	t.Helper()
	t.Setenv("MSB_PATH", paths.msb)
	t.Setenv("MSB_LIBKRUNFW_PATH", paths.firmware)
	version, err := probeVersion(paths.msb)
	require.NoError(t, err)
	require.Contains(t, []string{"msb 0.7.13", "microsandbox 0.7.13"}, strings.TrimSpace(version))
	pair, err := microsandbox.ResolveRuntime(microsandbox.RuntimeConfig{
		Home: paths.home, MSBPath: paths.msb, LibkrunfwPath: paths.firmware,
	})
	require.NoError(t, err)
	require.Equal(t, paths.msb, pair.MSBPath)
	require.Equal(t, paths.firmware, pair.LibkrunfwPath)

	output := runQualifiedAdapter(t, paths.adapter)
	var result runtimeCheckResult
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.DisallowUnknownFields()
	require.NoError(t, decoder.Decode(&result))
	require.ErrorIs(t, decoder.Decode(new(any)), io.EOF)
	require.Equal(t, runtimeCheckResult{
		SchemaVersion: 1, MSBPath: paths.msb, LibkrunfwPath: paths.firmware,
		Version: "0.7.13", Compatible: true,
	}, result)
}

func downloadQualifiedArchive(t *testing.T, root string) *os.File {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Minute}
	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		qualifiedArchiveURL,
		nil,
	)
	require.NoError(t, err)
	response, err := client.Do(request)
	require.NoError(t, err)
	defer func() { require.NoError(t, response.Body.Close()) }()
	require.Equal(t, http.StatusOK, response.StatusCode)
	archive, err := os.CreateTemp(root, "archive-*.tar.gz")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, archive.Close()) })
	hash := sha256.New()
	count, err := io.Copy(
		io.MultiWriter(archive, hash),
		io.LimitReader(response.Body, qualifiedArchiveBytes+1),
	)
	require.NoError(t, err)
	require.EqualValues(t, qualifiedArchiveBytes, count)
	require.Equal(t, qualifiedArchiveSHA256, hex.EncodeToString(hash.Sum(nil)))
	_, err = archive.Seek(0, io.SeekStart)
	require.NoError(t, err)
	return archive
}

func extractQualifiedPair(
	t *testing.T,
	archive *os.File,
	directory string,
) (msbPath, firmwarePath string) {
	t.Helper()
	input := bufio.NewReader(archive)
	compressed, err := gzip.NewReader(input)
	require.NoError(t, err)
	compressed.Multistream(false)
	defer func() { require.NoError(t, compressed.Close()) }()
	limited := &io.LimitedReader{R: compressed, N: qualifiedExpandedBytes + 1}
	reader := tar.NewReader(limited)
	root, err := os.OpenRoot(directory)
	require.NoError(t, err)
	defer func() { require.NoError(t, root.Close()) }()
	expected := map[string]int64{
		qualifiedMSBName:      qualifiedMSBBytes,
		qualifiedFirmwareName: qualifiedFirmwareBytes,
	}
	seen := make(map[string]bool, len(expected))
	for {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		require.NoError(t, nextErr)
		want, ok := expected[header.Name]
		require.True(t, ok)
		require.False(t, seen[header.Name])
		require.Equal(t, byte(tar.TypeReg), header.Typeflag)
		require.Empty(t, header.Linkname)
		require.Empty(t, header.PAXRecords)
		require.Equal(t, want, header.Size)
		seen[header.Name] = true
		writeQualifiedMember(t, root, reader, header)
	}
	require.Len(t, seen, len(expected))
	verifyQualifiedArchiveTail(t, limited, input)
	return filepath.Join(
			directory,
			qualifiedMSBName,
		), filepath.Join(
			directory,
			qualifiedFirmwareName,
		)
}

func writeQualifiedMember(t *testing.T, root *os.Root, reader *tar.Reader, header *tar.Header) {
	t.Helper()
	mode := os.FileMode(0o600)
	if header.Name == qualifiedMSBName {
		mode = 0o700
	}
	file, err := root.OpenFile(header.Name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	require.NoError(t, err)
	count, copyErr := io.CopyN(file, reader, header.Size)
	closeErr := file.Close()
	require.NoError(t, copyErr)
	require.NoError(t, closeErr)
	require.Equal(t, header.Size, count)
}

func verifyQualifiedArchiveTail(
	t *testing.T,
	expanded *io.LimitedReader,
	compressed *bufio.Reader,
) {
	t.Helper()
	buffer := make([]byte, 32*1024)
	for {
		count, err := expanded.Read(buffer)
		for _, value := range buffer[:count] {
			require.Zero(t, value)
		}
		require.Positive(t, expanded.N)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}
	_, err := compressed.Peek(1)
	require.ErrorIs(t, err, io.EOF)
}

func runQualifiedAdapter(t *testing.T, binary string) []byte {
	t.Helper()
	command := &exec.Cmd{
		Path: binary, Args: []string{binary, "runtime-check"}, Env: os.Environ(),
		WaitDelay: time.Second,
	}
	var stdout, stderr boundedOutput
	command.Stdout, command.Stderr = &stdout, &stderr
	require.NoError(t, command.Start())
	timer := time.AfterFunc(20*time.Second, func() { _ = command.Process.Kill() })
	defer timer.Stop()
	err := command.Wait()
	require.False(t, stdout.overflow || stderr.overflow)
	require.NoError(t, err, stderr.buffer.String())
	return stdout.buffer.Bytes()
}
