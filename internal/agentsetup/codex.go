package agentsetup

import (
	"regexp"
	"strings"
)

// Codex runs hooks from hooks.json only when its config.toml enables them
// with `[features] hooks = true`. The flag is edited line by line so the
// rest of the file keeps its bytes; it is never turned off again because
// other hooks may rely on it.

var (
	tomlTableHeader = regexp.MustCompile(`^\s*(\[{1,2})([^\]]+)(\]{1,2})\s*(?:#.*)?$`)
	tomlHooksFlag   = regexp.MustCompile(`^\s*hooks\s*=\s*(true|false)\s*(?:#.*)?$`)
)

// enableCodexHooks returns content with `hooks = true` in [features].
func enableCodexHooks(content string) (string, bool) {
	nl := "\n"
	if strings.Contains(content, "\r\n") {
		nl = "\r\n"
	}
	if strings.TrimSpace(content) == "" {
		return "[features]" + nl + "hooks = true" + nl, true
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
				return strings.Join(lines, "\n"), true
			}
			inFeatures = strings.TrimSpace(m[2]) == "features"
			sawFeatures = sawFeatures || inFeatures
			continue
		}
		if !inFeatures {
			continue
		}
		if m := tomlHooksFlag.FindStringSubmatch(line); m != nil {
			if m[1] == "true" {
				return content, false
			}
			lines[i] = strings.Replace(line, "false", "true", 1)
			return strings.Join(lines, "\n"), true
		}
	}
	if sawFeatures {
		if !strings.HasSuffix(content, nl) {
			content += nl
		}
		return content + "hooks = true" + nl, true
	}
	sep := nl
	if !strings.HasSuffix(content, nl) {
		sep = nl + nl
	}
	return content + sep + "[features]" + nl + "hooks = true" + nl, true
}

// CodexHooksEnabled reports whether config.toml content enables hooks.
func CodexHooksEnabled(content string) bool {
	_, changed := enableCodexHooks(content)
	return !changed
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
	out, changed := enableCodexHooks(string(f.Data))
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
	return f.Exists && CodexHooksEnabled(string(f.Data)), nil
}
