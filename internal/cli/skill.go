package cli

import (
	"context"
	_ "embed"
	"flag"
	"io"
)

// skillMarkdown is SKILL.md: an agent skill, in the Agent Skills format
// (YAML frontmatter with name and description, then Markdown), that
// teaches an AI agent these commands. TestSkillListsEveryCommand and
// TestSkillListsEveryErrorCode keep it in step with the code.
//
//go:embed SKILL.md
var skillMarkdown string

// setupSkill is `slk skill`: it prints SKILL.md, so an agent (or the
// user installing the skill) gets the version that matches this
// binary. Under --json the file is the "skill" field.
func setupSkill(*flag.FlagSet) runFunc {
	return func(ctx context.Context, inv *Invocation) error {
		doc := struct {
			Skill string `json:"skill"`
		}{skillMarkdown}
		return inv.out.result(doc, func(w io.Writer) error {
			_, err := io.WriteString(w, skillMarkdown)
			return err
		})
	}
}
