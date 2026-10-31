package cli

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestSkill_PrintsTheEmbeddedFile(t *testing.T) {
	stdout, _, exit := runCLI(t, []string{"skill"})
	if exit != exitOK || stdout != skillMarkdown {
		t.Errorf("exit %d; stdout is not SKILL.md", exit)
	}
	stdout, _, _ = runCLI(t, []string{"skill", "--json"})
	var doc struct{ Skill string }
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil || doc.Skill != skillMarkdown {
		t.Errorf("--json: the skill field is not SKILL.md (err %v)", err)
	}
}

// TestSkill_HasAgentSkillsFrontmatter: an agent runtime finds a skill
// by the name and description in its YAML frontmatter.
func TestSkill_HasAgentSkillsFrontmatter(t *testing.T) {
	front, _, ok := strings.Cut(strings.TrimPrefix(skillMarkdown, "---\n"), "\n---\n")
	if !strings.HasPrefix(skillMarkdown, "---\n") || !ok {
		t.Fatal("SKILL.md does not start with a --- frontmatter block")
	}
	if !strings.Contains(front, "name: slk\n") || !strings.Contains(front, "description: ") {
		t.Errorf("frontmatter needs name: slk and a description:\n%s", front)
	}
}

// TestSkillListsEveryCommand: SKILL.md is how an agent learns the
// commands, so a command missing from it does not exist for the agent.
func TestSkillListsEveryCommand(t *testing.T) {
	for _, c := range commands {
		if name := "`slk " + strings.Join(c.Path, " ") + "`"; !strings.Contains(skillMarkdown, name) {
			t.Errorf("SKILL.md does not mention %s", name)
		}
	}
}

// TestSkillListsEveryErrorCode: agents branch on error codes, so each
// one must be documented, with its exit code.
func TestSkillListsEveryErrorCode(t *testing.T) {
	for code, exit := range errorExits {
		row := "| `" + code + "` | " + strconv.Itoa(exit) + " |"
		if !strings.Contains(skillMarkdown, row) {
			t.Errorf("SKILL.md has no row %q", row)
		}
	}
}
