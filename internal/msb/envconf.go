package msb

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
)

const envConfPrefix = "DEVSY_MSB_ENV_"

// envConf carries guest environment to `msb run` without putting values on its
// command line, where other local users can read them with ps.
type envConf struct {
	path    string
	environ []string
}

// writeEnvConf writes a private --runtime-conf file mapping each guest variable
// to a ${DEVSY_MSB_ENV_n} reference, and returns the host variables that hold
// the values. msb substitutes references once and has no escape for "${", so
// values go through the environment instead of the file. The file holds no
// secrets. An empty env yields a zero envConf.
func writeEnvConf(env map[string]string) (envConf, error) {
	if len(env) == 0 {
		return envConf{}, nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	refs := make(map[string]string, len(env))
	environ := make([]string, 0, len(env))
	for i, k := range keys {
		name := fmt.Sprintf("%s%d", envConfPrefix, i)
		refs[k] = "${" + name + "}"
		environ = append(environ, name+"="+env[k])
	}
	data, err := json.Marshal(map[string]any{"env": refs})
	if err != nil {
		return envConf{}, fmt.Errorf("encode microsandbox environment: %w", err)
	}
	f, err := os.CreateTemp("", "devsy-msb-env-*.json")
	if err != nil {
		return envConf{}, fmt.Errorf("create microsandbox environment file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return envConf{}, fmt.Errorf("write microsandbox environment file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return envConf{}, fmt.Errorf("write microsandbox environment file: %w", err)
	}
	return envConf{path: f.Name(), environ: environ}, nil
}

func (e envConf) remove() {
	if e.path != "" {
		_ = os.Remove(e.path)
	}
}

// values returns the secret values for error redaction.
func (e envConf) values() []string {
	out := make([]string, 0, len(e.environ))
	for _, kv := range e.environ {
		if _, v, ok := strings.Cut(kv, "="); ok {
			out = append(out, v)
		}
	}
	return out
}

// redactSecrets masks all values in one pass, longest first, so a value that is
// a prefix of another cannot leave part of the longer one visible.
func redactSecrets(text string, secrets []string) string {
	sorted := slices.Clone(secrets)
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	pairs := make([]string, 0, 2*len(sorted))
	for _, v := range sorted {
		if v != "" {
			pairs = append(pairs, v, "***")
		}
	}
	if len(pairs) == 0 {
		return text
	}
	return strings.NewReplacer(pairs...).Replace(text)
}
