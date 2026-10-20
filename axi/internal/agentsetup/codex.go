package agentsetup

import (
	"errors"
	"reflect"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/gehariharan/wasync/axi/internal/contract"
)

// Codex runs hooks from hooks.json only when its config.toml enables them
// with `[features] hooks = true`. The flag is edited line by line so the
// rest of the file keeps its bytes; it is never turned off again because
// other hooks may rely on it.

var (
	tomlTableHeader = regexp.MustCompile(`^\s*(\[{1,2})([^\]]+)(\]{1,2})\s*(?:#.*)?$`)
	tomlHooksFlag   = regexp.MustCompile(`^\s*hooks\s*=\s*false\s*(?:#.*)?$`)
)

// enableCodexHooks returns content with `hooks = true` in [features].
func enableCodexHooks(content string) (string, bool, error) {
	var expected map[string]any
	if _, err := toml.Decode(content, &expected); err != nil {
		return "", false, err
	}
	features, ok := expected["features"].(map[string]any)
	if !ok {
		if _, exists := expected["features"]; exists {
			return "", false, errors.New("features is not a table")
		}
		features = map[string]any{}
		expected["features"] = features
	}
	if hooks, exists := features["hooks"]; exists {
		enabled, ok := hooks.(bool)
		if !ok {
			return "", false, errors.New("features.hooks is not a boolean")
		}
		if enabled {
			return content, false, nil
		}
	}
	features["hooks"] = true
	out := editCodexHooks(content)
	var actual map[string]any
	if _, err := toml.Decode(out, &actual); err != nil || !reflect.DeepEqual(expected, actual) {
		return "", false, errors.New("unsupported features.hooks representation")
	}
	return out, true, nil
}

func editCodexHooks(content string) string {
	nl := "\n"
	if strings.Contains(content, "\r\n") {
		nl = "\r\n"
	}
	if strings.TrimSpace(content) == "" {
		return "[features]" + nl + "hooks = true" + nl
	}
	lines := strings.Split(content, "\n") // a "\r" stays on its line
	inFeatures, sawFeatures := false, false
	for i, line := range lines {
		if m := tomlTableHeader.FindStringSubmatch(line); m != nil {
			if !(m[1] == "[" && m[3] == "]") && !(m[1] == "[[" && m[3] == "]]") {
				continue
			}
			if inFeatures {
				flag := "hooks = true" + strings.TrimSuffix(nl, "\n")
				lines = append(lines[:i], append([]string{flag}, lines[i:]...)...)
				return strings.Join(lines, "\n")
			}
			inFeatures = strings.TrimSpace(m[2]) == "features"
			sawFeatures = sawFeatures || inFeatures
			continue
		}
		if !inFeatures {
			continue
		}
		if tomlHooksFlag.MatchString(line) {
			lines[i] = strings.Replace(line, "false", "true", 1)
			return strings.Join(lines, "\n")
		}
	}
	if sawFeatures {
		if !strings.HasSuffix(content, nl) {
			content += nl
		}
		return content + "hooks = true" + nl
	}
	sep := nl
	if !strings.HasSuffix(content, nl) {
		sep = nl + nl
	}
	return content + sep + "[features]" + nl + "hooks = true" + nl
}

// PlanCodexFeature plans enabling Codex hooks in config.toml under root.
func PlanCodexFeature(root string) (Change, error) {
	path := CodexConfigPath(root)
	c := Change{Agent: "codex", Item: ItemHooksFeature, Path: path, Action: ActionNone}
	f, err := readFile(path)
	if err != nil {
		return c, err
	}
	c.target = f.Target
	out, changed, err := enableCodexHooks(string(f.Data))
	if err != nil {
		return c, &contract.Error{Code: CodeConfigInvalid, Message: path + " cannot be edited safely: " + err.Error(),
			Help: "enable features.hooks in config.toml manually, then rerun the command"}
	}
	if !changed {
		return c, nil
	}
	c.Action, c.data = ActionUpdate, []byte(out)
	if !f.Exists {
		c.Action = ActionAdd
	}
	return c, nil
}

// InspectCodexFeature reports whether Codex hooks are enabled under root.
func InspectCodexFeature(root string) (bool, error) {
	f, err := readFile(CodexConfigPath(root))
	if err != nil {
		return false, err
	}
	var cfg map[string]any
	if _, err := toml.Decode(string(f.Data), &cfg); err != nil {
		return false, err
	}
	features, _ := cfg["features"].(map[string]any)
	enabled, _ := features["hooks"].(bool)
	return f.Exists && enabled, nil
}
