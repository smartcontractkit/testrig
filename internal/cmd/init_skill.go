package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/smartcontractkit/testrig/internal/assets"
)

var initSkillCmd = &cobra.Command{
	Use:   "init-skill",
	Short: "Install the fix-flaky-tests skill into your repository",
	Long: `Install the fix-flaky-tests skill into your repository.
This creates the .agents/skills/fix-flaky-tests directory with the default SKILL.md
and a references/known-patterns directory for memory bank storage.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		targetDir := ".agents/skills/fix-flaky-tests"

		fmt.Printf("Installing skill to %s...\n", targetDir)

		if err := os.MkdirAll(targetDir, 0o750); err != nil {
			return fmt.Errorf("failed to create target directory: %w", err)
		}

		// Ensure known-patterns directory exists
		patternsDir := filepath.Join(targetDir, "references", "known-patterns")
		if err := os.MkdirAll(patternsDir, 0o750); err != nil {
			return fmt.Errorf("failed to create known-patterns directory: %w", err)
		}

		// Copy SKILL.md from embedded assets
		content, err := assets.SkillsFS.ReadFile("skills/fix-flaky-tests/SKILL.md")
		if err != nil {
			return fmt.Errorf("failed to read embedded skill: %w", err)
		}

		skillPath := filepath.Join(targetDir, "SKILL.md")
		if err := os.WriteFile(skillPath, content, 0o600); err != nil {
			return fmt.Errorf("failed to write SKILL.md: %w", err)
		}

		// Scaffold custom hook files
		hookFiles := map[string]string{
			"CONTEXT.md":     "<!-- Add repository-specific context for fix-flaky-tests here -->\n",
			"custom-init.md": "<!-- Add custom initialization steps for fix-flaky-tests here -->\n",
			"custom-loop.md": "<!-- Add custom loop constraints or instructions for fix-flaky-tests here -->\n",
		}

		for name, data := range hookFiles {
			path := filepath.Join(targetDir, "references", name)
			// Only create if it doesn't exist so we don't overwrite user customizations
			if _, err := os.Stat(path); os.IsNotExist(err) {
				if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
					return fmt.Errorf("failed to write %s: %w", name, err)
				}
			}
		}

		fmt.Println("Skill successfully installed!")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initSkillCmd)
}
