package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

var log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

// LoginFunc is a callback for login operations (set by main package)
var LoginFunc func(profile string, opts LoginOptions)

// LoginAllFunc is a callback for login all profiles (set by main package)
var LoginAllFunc func(opts LoginOptions)

// LoginMultipleFunc is a callback for login multiple profiles (set by main package)
var LoginMultipleFunc func(profiles []string, opts LoginOptions)

// LoginOptions holds options for login operation
type LoginOptions struct {
	Ctx              interface{} // context.Context, but using interface{} to avoid circular import
	NoPrompt         bool
	IsGui            bool
	IsDebug          bool
	ShowBrowser      bool
	DisableLeakless  bool
	FastPass         bool
	UseSystemBrowser bool
	AwsNoVerifySsl   bool
	ForceRefresh     bool
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to AWS via Azure AD",
	Long: `Log in to AWS using Azure Active Directory SSO authentication.

This command handles the full Azure AD login flow (including MFA) and stores
temporary AWS credentials for use with the AWS CLI and SDKs.`,
	Run: func(cmd *cobra.Command, args []string) {
		runLogin(cmd)
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)

	// Add login-specific flags
	loginCmd.Flags().BoolP("all-profiles", "a", false, "Run for all configured profiles")
	loginCmd.Flags().BoolP("force-refresh", "f", false, "Force credential refresh, even if still valid")
	loginCmd.Flags().StringP("mode", "m", "gui", "Display mode: 'gui' (default), 'cli' (headless), or 'debug' (visible, no auto-fill)")
	loginCmd.Flags().Bool("no-verify-ssl", false, "Disable SSL verification for AWS connections")
	loginCmd.Flags().Bool("no-prompt", true, "Do not prompt for input and accept default choices")
	loginCmd.Flags().Bool("disable-leakless", false, "Disable leakless mode (troubleshooting)")
	loginCmd.Flags().Bool("fastpass", false, "Use Okta FastPass verification")
	loginCmd.Flags().Bool("system-browser", false, "Use system browser instead of embedded")

	// Register completion functions
	loginCmd.RegisterFlagCompletionFunc("mode", completeMode)
}

func runLogin(cmd *cobra.Command) {
	// Get flags - try login command first, then root command (for backward compatibility)
	profile, _ := cmd.Flags().GetString("profile")
	if profile == "" {
		profile, _ = cmd.Root().PersistentFlags().GetString("profile")
	}

	allProfiles := getFlagBool(cmd, "all-profiles")
	forceRefresh := getFlagBool(cmd, "force-refresh")
	mode := getFlagString(cmd, "mode")
	noVerifySSL := getFlagBool(cmd, "no-verify-ssl")
	noPrompt := getFlagBool(cmd, "no-prompt")
	disableLeakless := getFlagBool(cmd, "disable-leakless")
	fastPass := getFlagBool(cmd, "fastpass")
	useSystemBrowser := getFlagBool(cmd, "system-browser")

	isGui := mode == "gui"
	isDebug := mode == "debug"
	showBrowser := mode == "gui" || mode == "debug"

	// Parse profile(s) - support comma-separated values
	var profileNames []string
	if profile != "" {
		for _, p := range strings.Split(profile, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				profileNames = append(profileNames, p)
			}
		}
	} else if osAWSProfile := os.Getenv("AWS_PROFILE"); osAWSProfile != "" {
		profileNames = []string{osAWSProfile}
	} else {
		profileNames = []string{"default"}
	}

	opts := LoginOptions{
		Ctx:              appCtx,
		NoPrompt:         noPrompt,
		IsGui:            isGui,
		IsDebug:          isDebug,
		ShowBrowser:      showBrowser,
		DisableLeakless:  disableLeakless,
		FastPass:         fastPass,
		UseSystemBrowser: useSystemBrowser,
		AwsNoVerifySsl:   noVerifySSL,
		ForceRefresh:     forceRefresh,
	}

	startStdinMonitor()
	log.Info().Msg("Press 'q' + Enter to quit")

	done := make(chan struct{})
	go func() {
		if allProfiles {
			if LoginAllFunc != nil {
				LoginAllFunc(opts)
			} else {
				fmt.Fprintln(os.Stderr, "Error: LoginAllFunc not set")
			}
		} else if len(profileNames) > 1 {
			if LoginMultipleFunc != nil {
				LoginMultipleFunc(profileNames, opts)
			} else {
				fmt.Fprintln(os.Stderr, "Error: LoginMultipleFunc not set")
			}
		} else {
			if LoginFunc != nil {
				LoginFunc(profileNames[0], opts)
			} else {
				fmt.Fprintln(os.Stderr, "Error: LoginFunc not set")
			}
		}
		close(done)
	}()

	// Wait for completion or context cancellation
	select {
	case <-done:
		// Normal completion
	case <-appCtx.Done():
		// Signal received
	}
}

// getFlagBool gets a bool flag, trying the command first then root
func getFlagBool(cmd *cobra.Command, name string) bool {
	if cmd.Flags().Changed(name) {
		val, _ := cmd.Flags().GetBool(name)
		return val
	}
	if cmd.Root().Flags().Changed(name) {
		val, _ := cmd.Root().Flags().GetBool(name)
		return val
	}
	// Return default from login command
	val, _ := cmd.Flags().GetBool(name)
	return val
}

// getFlagString gets a string flag, trying the command first then root
func getFlagString(cmd *cobra.Command, name string) string {
	if cmd.Flags().Changed(name) {
		val, _ := cmd.Flags().GetString(name)
		return val
	}
	if cmd.Root().Flags().Changed(name) {
		val, _ := cmd.Root().Flags().GetString(name)
		return val
	}
	// Return default from login command
	val, _ := cmd.Flags().GetString(name)
	return val
}
