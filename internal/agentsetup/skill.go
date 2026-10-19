package agentsetup

import (
	"bufio"
	"bytes"
	"strings"

	"github.com/gehariharan/whatsapp-axi/internal/contract"
)

// SkillFrontmatterName returns the `name:` of a SKILL.md's YAML
// frontmatter, or "" when it has none.
func SkillFrontmatterName(data []byte) string {
	s := bufio.NewScanner(bytes.NewReader(data))
	if !s.Scan() || strings.TrimSpace(s.Text()) != "---" {
		return ""
	}
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "---" {
			return ""
		}
		if v, ok := strings.CutPrefix(line, "name:"); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

// PlanSkill plans installing content as agent's whatsapp-axi skill under
// root, or removing it. A file at that path that is not the whatsapp-axi
// skill is never replaced or removed.
func PlanSkill(agent Agent, root string, content []byte, remove bool) (Change, error) {
	path := agent.SkillPath(root)
	c := Change{Agent: agent.Name, Item: ItemSkill, Path: path, Action: ActionNone}
	f, err := readFile(path)
	if err != nil {
		return c, err
	}
	c.target = f.Target
	if f.Exists && SkillFrontmatterName(f.Data) != SkillName {
		return c, &contract.Error{Code: CodeConfigInvalid, Message: path + " exists but is not the whatsapp-axi skill (its frontmatter has no `name: " + SkillName + "`)",
			Help: "move the file away, then rerun; whatsapp-axi only replaces or removes its own skill"}
	}
	switch {
	case remove && f.Exists:
		c.Action, c.delete = ActionRemove, true
	case remove:
	case !f.Exists:
		c.Action, c.data = ActionAdd, content
	case !bytes.Equal(f.Data, content):
		c.Action, c.data = ActionUpdate, content
	}
	return c, nil
}

// SkillStatus is what InspectSkill found.
type SkillStatus struct {
	Path      string
	Installed bool // the whatsapp-axi skill is there
	Current   bool // and matches this build's skill
	Err       error
}

// InspectSkill reports agent's whatsapp-axi skill under root.
func InspectSkill(agent Agent, root string, content []byte) SkillStatus {
	s := SkillStatus{Path: agent.SkillPath(root)}
	f, err := readFile(s.Path)
	if err != nil {
		s.Err = err
		return s
	}
	if !f.Exists {
		return s
	}
	if SkillFrontmatterName(f.Data) != SkillName {
		s.Err = &contract.Error{Code: CodeConfigInvalid, Message: s.Path + " is not the whatsapp-axi skill"}
		return s
	}
	s.Installed, s.Current = true, bytes.Equal(f.Data, content)
	return s
}
