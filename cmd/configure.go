package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// ConfigureFunc is a callback for configure operations (set by main package)
var ConfigureFunc func(profile string)

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Configure an AWS profile for Azure AD login",
	Long: `Configure an AWS profile for Azure AD SSO authentication.

This command runs the configuration wizard to set up Azure AD tenant ID,
App ID URI, and other settings for the specified profile.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Get profile flag
		profile, _ := cmd.Flags().GetString("profile")
		if profile == "" {
			profile, _ = cmd.Root().PersistentFlags().GetString("profile")
		}
		if profile == "" {
			if osAWSProfile := os.Getenv("AWS_PROFILE"); osAWSProfile != "" {
				profile = osAWSProfile
			} else {
				profile = "default"
			}
		}

		if ConfigureFunc != nil {
			ConfigureFunc(profile)
		} else {
			fmt.Fprintln(os.Stderr, "Error: ConfigureFunc not set")
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(configureCmd)
}
