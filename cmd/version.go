package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  `Print the version, git commit, and build date of go-aws-azure-login.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("go-aws-azure-login %s\n", Version)
		if GitCommit != "unknown" {
			fmt.Printf("  Git commit: %s\n", GitCommit)
		}
		if BuildDate != "unknown" {
			fmt.Printf("  Build date: %s\n", BuildDate)
		}
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
