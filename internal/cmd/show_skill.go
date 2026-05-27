package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/smartcontractkit/testrig/internal/assets"
)

var showSkillCmd = &cobra.Command{
	Use:   "show-skill",
	Short: "Print the fix-flaky-tests skill to stdout",
	Long: `Print the fix-flaky-tests skill to stdout so it can be dynamically loaded into an AI agent session.
For example, you can pipe it to an agent or run it inside an interactive session.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		content, err := assets.SkillsFS.ReadFile("skills/fix-flaky-tests/SKILL.md")
		if err != nil {
			return fmt.Errorf("failed to read embedded skill: %w", err)
		}

		// Write directly to os.Stdout so it can be cleanly piped
		_, err = os.Stdout.Write(content)
		if err != nil {
			return fmt.Errorf("failed to write skill content: %w", err)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(showSkillCmd)
}
