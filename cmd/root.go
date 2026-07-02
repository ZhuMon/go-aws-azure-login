package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

var (
	// Version information (set at build time via ldflags, or auto-detected)
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

func init() {
	// Auto-detect version from build info (works with go install)
	if info, ok := debug.ReadBuildInfo(); ok {
		if Version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			Version = info.Main.Version
		}
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if GitCommit == "unknown" && setting.Value != "" {
					GitCommit = setting.Value
					if len(GitCommit) > 7 {
						GitCommit = GitCommit[:7]
					}
				}
			case "vcs.time":
				if BuildDate == "unknown" && setting.Value != "" {
					BuildDate = setting.Value
				}
			}
		}
	}
}

// Global context for graceful shutdown
var appCtx context.Context
var appCancel context.CancelFunc
var sigChan = make(chan os.Signal, 1)

// Exit cleanups run before os.Exit on the signal / 'q' / normal paths. The CLI
// exits via os.Exit, which skips deferred functions, so the browser's cleanup
// (which kills the launched process) would otherwise never run and the browser
// leaks. Callers register their cleanup here; runExitCleanup drains them once.
var (
	exitCleanupMu   sync.Mutex
	exitCleanups    []func()
	exitCleanupOnce sync.Once
)

// RegisterCleanup adds a function to run before the process exits. It is safe to
// call from any package (e.g. main after creating the browser).
func RegisterCleanup(fn func()) {
	if fn == nil {
		return
	}
	exitCleanupMu.Lock()
	defer exitCleanupMu.Unlock()
	exitCleanups = append(exitCleanups, fn)
}

// runExitCleanup runs all registered cleanups exactly once, in registration
// order. Guarded by sync.Once so the signal path and the normal path can both
// call it without double-running (e.g. closing the browser twice).
func runExitCleanup() {
	exitCleanupOnce.Do(func() {
		exitCleanupMu.Lock()
		fns := exitCleanups
		exitCleanupMu.Unlock()
		for _, fn := range fns {
			fn()
		}
	})
}

var rootCmd = &cobra.Command{
	Use:   "go-aws-azure-login",
	Short: "AWS login via Azure AD SSO",
	Long: `A command-line tool for logging into AWS using Azure Active Directory SSO authentication.

If your organization uses Azure Active Directory for SSO login to the AWS console,
this tool lets you authenticate from the command line. It handles the full Azure AD
login flow (including MFA) and stores temporary AWS credentials for use with the
AWS CLI and SDKs.`,
	// Apply log level before any subcommand runs
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		levelStr, _ := cmd.Flags().GetString("log-level")
		level, err := zerolog.ParseLevel(levelStr)
		if err != nil {
			return fmt.Errorf("invalid --log-level %q (use trace, debug, info, warn, error)", levelStr)
		}
		zerolog.SetGlobalLevel(level)
		return nil
	},
	// Run login command by default when no subcommand is specified
	Run: func(cmd *cobra.Command, args []string) {
		loginCmd.Run(cmd, args)
	},
}

func init() {
	// Create cancellable context
	appCtx, appCancel = context.WithCancel(context.Background())

	// Register signal handler
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)

	// Handle signals in goroutine
	go func() {
		for sig := range sigChan {
			fmt.Fprintf(os.Stderr, "\nReceived signal: %v, shutting down...\n", sig)
			appCancel()
			runExitCleanup()
			os.Exit(0)
		}
	}()

	// Add persistent flags (available to all subcommands)
	rootCmd.PersistentFlags().StringP("profile", "p", "", "Profile name(s) to use. Use comma-separated values for multiple profiles (e.g., -p dev,staging,prod)")
	rootCmd.PersistentFlags().String("log-level", "info", "Log verbosity: trace, debug, info, warn, error")

	// Copy flags from login command to root for backward compatibility
	rootCmd.Flags().BoolP("all-profiles", "a", false, "Run for all configured profiles")
	rootCmd.Flags().BoolP("force-refresh", "f", false, "Force credential refresh, even if still valid")
	rootCmd.Flags().StringP("mode", "m", "gui", "Display mode: 'gui' (default), 'cli' (headless), or 'debug' (visible, no auto-fill)")
	rootCmd.Flags().Bool("no-verify-ssl", false, "Disable SSL verification for AWS connections")
	rootCmd.Flags().Bool("no-prompt", true, "Do not prompt for input and accept default choices")
	rootCmd.Flags().Bool("disable-leakless", false, "Disable leakless mode (troubleshooting)")
	rootCmd.Flags().Bool("fastpass", false, "Use Okta FastPass verification")
	rootCmd.Flags().Bool("system-browser", false, "Use system browser instead of embedded")
	rootCmd.Flags().BoolP("continue-on-error", "k", false, "Continue with the next profile when one fails (batch mode only)")

	// Register completion function for profile flag
	rootCmd.RegisterFlagCompletionFunc("profile", completeProfiles)
	rootCmd.RegisterFlagCompletionFunc("mode", completeMode)
}

// startStdinMonitor monitors stdin for 'q' to quit (workaround for Chromium intercepting Ctrl+C)
func startStdinMonitor() {
	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return // stdin closed
			}
			if strings.TrimSpace(strings.ToLower(line)) == "q" {
				fmt.Fprintln(os.Stderr, "\nQuitting...")
				appCancel()
				runExitCleanup()
				os.Exit(0)
			}
		}
	}()
}

// GetContext returns the application context for graceful shutdown
func GetContext() context.Context {
	return appCtx
}

// GetCancel returns the cancel function for graceful shutdown
func GetCancel() context.CancelFunc {
	return appCancel
}

// Execute runs the root command
func Execute() {
	defer appCancel()
	if err := rootCmd.Execute(); err != nil {
		runExitCleanup()
		os.Exit(1)
	}
	if loginErr != nil {
		runExitCleanup()
		log.Error().Err(loginErr).Msg("Login failed")
		os.Exit(1)
	}
	// Normal success path: os.Exit is not called here, but run cleanup anyway so
	// the browser is killed rather than left for defer (which the signal path
	// may preempt on a later run).
	runExitCleanup()
}
