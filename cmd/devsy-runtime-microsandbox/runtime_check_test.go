package main

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/devsy-org/devsy-provider-microsandbox/internal/config"
	"github.com/stretchr/testify/require"
	microsandbox "github.com/superradcompany/microsandbox/sdk/go"
)

const (
	runtimeTestFirmwarePath = "/runtime/fw"
	runtimeTestMSBPath      = "/runtime/msb"
)

func TestRuntimeCheck(t *testing.T) {
	cfg, err := config.Load(func(string) string { return "" })
	require.NoError(t, err)
	cfg.MSBPath, cfg.LibkrunfwPath = runtimeTestMSBPath, runtimeTestFirmwarePath
	pair := microsandbox.ResolvedRuntime{MSBPath: cfg.MSBPath, LibkrunfwPath: cfg.LibkrunfwPath}
	resolve := func(c microsandbox.RuntimeConfig) (microsandbox.ResolvedRuntime, error) {
		require.Equal(t, cfg.MSBPath, c.MSBPath)
		require.Equal(t, cfg.LibkrunfwPath, c.LibkrunfwPath)
		return pair, nil
	}
	result, err := checkRuntime(cfg, resolve, func(path string) (string, error) {
		require.Equal(t, cfg.MSBPath, path)
		return "msb 0.7.13", nil
	})
	require.NoError(t, err)
	data, err := json.Marshal(result)
	require.NoError(t, err)
	require.JSONEq(
		t,
		`{"schemaVersion":1,"msbPath":"/runtime/msb","libkrunfwPath":"/runtime/fw","version":"0.7.13","compatible":true}`,
		string(data),
	)
	result, err = checkRuntime(
		cfg,
		resolve,
		func(string) (string, error) { return "msb 0.7.12", nil },
	)
	require.NoError(t, err)
	require.False(t, result.Compatible)
	_, err = checkRuntime(
		cfg,
		resolve,
		func(string) (string, error) { return "", errors.New("failed") },
	)
	require.Error(t, err)
	cfg.LibkrunfwPath = ""
	_, err = checkRuntime(cfg, resolve, nil)
	require.ErrorContains(t, err, "must be set together")
}

func TestRuntimeCheckRejectsMismatch(t *testing.T) {
	cfg, err := config.Load(func(string) string { return "" })
	require.NoError(t, err)
	cfg.MSBPath, cfg.LibkrunfwPath = runtimeTestMSBPath, runtimeTestFirmwarePath
	_, err = checkRuntime(
		cfg,
		func(microsandbox.RuntimeConfig) (microsandbox.ResolvedRuntime, error) {
			return microsandbox.ResolvedRuntime{
				MSBPath:       "/other/msb",
				LibkrunfwPath: cfg.LibkrunfwPath,
			}, nil
		},
		nil,
	)
	require.ErrorContains(t, err, "does not match")
}

func TestBoundedOutput(t *testing.T) {
	var output boundedOutput
	data := make([]byte, 64*1024+1)
	n, err := output.Write(data)
	require.NoError(t, err)
	require.Equal(t, len(data), n)
	require.Len(t, output.buffer.Bytes(), 64*1024)
	require.True(t, output.overflow)
}

func TestRuntimeCheckRejectsChangedPair(t *testing.T) {
	cfg, err := config.Load(func(string) string { return "" })
	require.NoError(t, err)
	calls := 0
	_, err = checkRuntime(
		cfg,
		func(microsandbox.RuntimeConfig) (microsandbox.ResolvedRuntime, error) {
			calls++
			if calls == 1 {
				return microsandbox.ResolvedRuntime{
					MSBPath:       runtimeTestMSBPath,
					LibkrunfwPath: runtimeTestFirmwarePath,
				}, nil
			}
			return microsandbox.ResolvedRuntime{
				MSBPath:       "/other/msb",
				LibkrunfwPath: runtimeTestFirmwarePath,
			}, nil
		},
		nil,
	)
	require.ErrorContains(t, err, "pair changed")
}
