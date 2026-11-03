package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/blang/semver/v4"
	"github.com/devsy-org/devsy-provider-microsandbox/internal/config"
	"github.com/devsy-org/devsy-provider-microsandbox/internal/msb"
	microsandbox "github.com/superradcompany/microsandbox/sdk/go"
)

var minimumManagedRuntimeVersion = semver.Version{Minor: 7, Patch: 7}

type runtimeCheckResult struct {
	SchemaVersion int    `json:"schemaVersion"`
	MSBPath       string `json:"msbPath"`
	LibkrunfwPath string `json:"libkrunfwPath"`
	Version       string `json:"version"`
	Compatible    bool   `json:"compatible"`
}

func checkRuntime(
	cfg config.Config,
	resolve func(microsandbox.RuntimeConfig) (microsandbox.ResolvedRuntime, error),
	version func(string) (string, error),
) (runtimeCheckResult, error) {
	pair, err := resolvePair(cfg, resolve)
	if err != nil {
		return runtimeCheckResult{}, err
	}
	raw, err := version(pair.MSBPath)
	if err != nil {
		return runtimeCheckResult{}, err
	}
	parsed, err := msb.ParseVersion(raw)
	if err != nil {
		return runtimeCheckResult{}, err
	}
	return runtimeCheckResult{
		SchemaVersion: 1,
		MSBPath:       pair.MSBPath,
		LibkrunfwPath: pair.LibkrunfwPath,
		Version:       parsed.String(),
		Compatible:    parsed.GTE(minimumManagedRuntimeVersion),
	}, nil
}

func resolvePair(
	cfg config.Config,
	resolve func(microsandbox.RuntimeConfig) (microsandbox.ResolvedRuntime, error),
) (microsandbox.ResolvedRuntime, error) {
	if err := cfg.Validate(); err != nil {
		return microsandbox.ResolvedRuntime{}, err
	}
	pair, err := resolve(
		microsandbox.RuntimeConfig{MSBPath: cfg.MSBPath, LibkrunfwPath: cfg.LibkrunfwPath},
	)
	if err != nil {
		return microsandbox.ResolvedRuntime{}, err
	}
	if !requestedPairMatches(cfg, pair) {
		return microsandbox.ResolvedRuntime{}, errors.New(
			"resolved runtime does not match requested pair",
		)
	}
	verified, err := resolve(
		microsandbox.RuntimeConfig{MSBPath: pair.MSBPath, LibkrunfwPath: pair.LibkrunfwPath},
	)
	if err != nil {
		return microsandbox.ResolvedRuntime{}, err
	}
	if verified.MSBPath != pair.MSBPath || verified.LibkrunfwPath != pair.LibkrunfwPath {
		return microsandbox.ResolvedRuntime{}, errors.New("resolved runtime pair changed")
	}
	return pair, nil
}

func requestedPairMatches(cfg config.Config, pair microsandbox.ResolvedRuntime) bool {
	if pair.MSBPath == "" || pair.LibkrunfwPath == "" {
		return false
	}
	return cfg.MSBPath == "" ||
		(pair.MSBPath == cfg.MSBPath && pair.LibkrunfwPath == cfg.LibkrunfwPath)
}

func runtimeCheck() int {
	cfg, err := config.Load(os.Getenv)
	if err == nil {
		var result runtimeCheckResult
		result, err = checkRuntime(cfg, microsandbox.ResolveRuntime, probeVersion)
		if err == nil {
			err = json.NewEncoder(os.Stdout).Encode(result)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

type boundedOutput struct {
	buffer   bytes.Buffer
	overflow bool
}

func (w *boundedOutput) Write(p []byte) (int, error) {
	const limit = 64 * 1024
	n := len(p)
	remaining := limit - w.buffer.Len()
	if len(p) > remaining {
		w.overflow = true
		p = p[:remaining]
	}
	_, _ = w.buffer.Write(p)
	return n, nil
}

func probeVersion(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.WaitDelay = time.Second
	var stdout, stderr boundedOutput
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", fmt.Errorf("runtime version probe: %w", ctx.Err())
	}
	if stdout.overflow || stderr.overflow {
		return "", errors.New("runtime version probe exceeded output limit")
	}
	if err != nil {
		return "", fmt.Errorf("runtime version probe: %w: %s", err, stderr.buffer.String())
	}
	return stdout.buffer.String(), nil
}
