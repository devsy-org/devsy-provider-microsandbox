package msb

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"
)

const (
	envConfPrefix   = "DEVSY_MSB_ENV_"
	labelConfPrefix = "DEVSY_MSB_LABEL_"
)

// envConf carries guest environment and sandbox labels to `msb run` without
// putting values on its command line, where other local users can read them with
// ps. Label values matter because the devcontainer metadata label embeds
// containerEnv and remoteEnv values.
type envConf struct {
	path    string
	environ []string
	secrets []string
}

// writeEnvConf writes a private --runtime-conf file mapping each guest variable
// and label to a ${DEVSY_MSB_...} reference, and returns the host variables that
// hold the values. msb substitutes references once and has no escape for "${",
// so values go through the environment instead of the file. The file holds no
// secrets. Empty env and labels yield a zero envConf.
func writeEnvConf(spec Spec) (envConf, error) {
	labels := make(map[string]string, len(spec.Labels)+len(spec.GeneratedLabels))
	maps.Copy(labels, spec.Labels)
	maps.Copy(labels, spec.GeneratedLabels)
	if len(spec.Env) == 0 && len(labels) == 0 {
		return envConf{}, nil
	}
	envRefs, envVars := confRefs(envConfPrefix, spec.Env)
	labelRefs, labelVars := confRefs(labelConfPrefix, labels)
	conf := map[string]any{}
	if len(envRefs) > 0 {
		conf["env"] = envRefs
	}
	if len(labelRefs) > 0 {
		conf["labels"] = labelRefs
	}
	path, err := writeConfFile(conf)
	if err != nil {
		return envConf{}, err
	}
	secrets := make([]string, 0, len(spec.Env)+len(spec.Labels))
	for _, value := range spec.Env {
		secrets = append(secrets, value)
	}
	// Keep request values even when generated metadata overrides the same key.
	for _, value := range spec.Labels {
		secrets = append(secrets, value)
	}
	return envConf{path: path, environ: append(envVars, labelVars...), secrets: secrets}, nil
}

func writeConfFile(conf map[string]any) (string, error) {
	data, err := json.Marshal(conf)
	if err != nil {
		return "", fmt.Errorf("encode microsandbox runtime configuration: %w", err)
	}
	f, err := os.CreateTemp("", "devsy-msb-env-*.json")
	if err != nil {
		return "", fmt.Errorf("create microsandbox runtime configuration file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("write microsandbox runtime configuration file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("write microsandbox runtime configuration file: %w", err)
	}
	return f.Name(), nil
}

// confRefs maps each key to a reference to a numbered host variable and returns
// the NAME=value assignments for those variables.
func confRefs(prefix string, values map[string]string) (map[string]string, []string) {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	refs := make(map[string]string, len(values))
	environ := make([]string, 0, len(values))
	for i, k := range keys {
		name := fmt.Sprintf("%s%d", prefix, i)
		refs[k] = "${" + name + "}"
		environ = append(environ, name+"="+values[k])
	}
	return refs, environ
}

func (e envConf) remove() {
	if e.path != "" {
		_ = os.Remove(e.path)
	}
}

// values returns the secret values for error redaction.
func (e envConf) values() []string {
	return e.secrets
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
