package cmd

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion script",
	Long: `Generate shell completion script for the specified shell.

To load completions:

Zsh (macOS):
  $ sudo mkdir -p /usr/local/share/zsh/site-functions
  $ go-aws-azure-login completion zsh | sudo tee /usr/local/share/zsh/site-functions/_go-aws-azure-login > /dev/null
  $ rm -f ~/.zcompdump* && exec zsh

Zsh (Linux):
  $ mkdir -p ~/.local/share/zsh/site-functions
  $ go-aws-azure-login completion zsh > ~/.local/share/zsh/site-functions/_go-aws-azure-login
  $ rm -f ~/.zcompdump* && exec zsh

Bash:
  # Linux:
  $ go-aws-azure-login completion bash > /etc/bash_completion.d/go-aws-azure-login
  # macOS:
  $ go-aws-azure-login completion bash > $(brew --prefix)/etc/bash_completion.d/go-aws-azure-login

Fish:
  $ go-aws-azure-login completion fish > ~/.config/fish/completions/go-aws-azure-login.fish

PowerShell:
  PS> go-aws-azure-login completion powershell | Out-String | Invoke-Expression
`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	Run: func(cmd *cobra.Command, args []string) {
		switch args[0] {
		case "bash":
			cmd.Root().GenBashCompletion(os.Stdout)
		case "zsh":
			cmd.Root().GenZshCompletion(os.Stdout)
		case "fish":
			cmd.Root().GenFishCompletion(os.Stdout, true)
		case "powershell":
			cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
		}
	},
}

func init() {
	rootCmd.AddCommand(completionCmd)
}

// completeProfiles provides completion for AWS profile names
func completeProfiles(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	profiles := parseAWSProfiles()
	return profiles, cobra.ShellCompDirectiveNoFileComp
}

// completeMode provides completion for mode flag
func completeMode(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return []string{"gui", "cli", "debug"}, cobra.ShellCompDirectiveNoFileComp
}

// parseAWSProfiles reads profile names from ~/.aws/config
func parseAWSProfiles() []string {
	var profiles []string

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return profiles
	}

	configPath := filepath.Join(homeDir, ".aws", "config")
	file, err := os.Open(configPath)
	if err != nil {
		return profiles
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[profile ") && strings.HasSuffix(line, "]") {
			// Extract profile name from "[profile name]"
			profileName := strings.TrimPrefix(line, "[profile ")
			profileName = strings.TrimSuffix(profileName, "]")
			profiles = append(profiles, profileName)
		} else if line == "[default]" {
			profiles = append(profiles, "default")
		}
	}

	return profiles
}
