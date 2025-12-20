package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/rs/zerolog"
)

// Global context for graceful shutdown
var appCtx context.Context
var appCancel context.CancelFunc
var sigChan = make(chan os.Signal, 1)

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
			os.Exit(0)
		}
	}()

	// Monitor stdin for 'q' to quit (workaround for Chromium intercepting Ctrl+C)
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
				os.Exit(0)
			}
		}
	}()
}

var log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

var (
	profile          string
	allProfiles      bool
	forceRefresh     bool
	configure        bool
	mode             string
	noVerifySSL      bool
	noPrompt         bool
	disableLeakless  bool
	fastPass         bool
	useSystemBrowser bool
)

func init() {
	const (
		profileDefaultValue          = ""
		profileUsage                 = "The name of the profile to log in with (or configure)"
		allProfilesDefaultValue      = false
		allProfilesUsage             = "Run for all configured profiles"
		forceRefreshDefaultValue     = false
		forceRefreshUsage            = "Force a credential refresh, even if they are still valid"
		configureDefaultValue        = false
		configureUsage               = "Configure the profile"
		modeDefaultValue             = "gui"
		modeUsage                    = "'gui' to perform the login through the Azure GUI (default, required for MFA with number matching), 'cli' to hide the login page and perform the login through the CLI (only works with push-based MFA), 'debug' to show the login page but perform the login through the CLI (useful to debug issues with the CLI login)"
		noVerifySSLDefaultValue      = false
		noVerifySSLUsage             = "Disable SSL Peer Verification for connections to AWS (no effect if behind proxy)"
		noPromptDefaultValue         = true
		noPromptUsage                = "Do not prompt for input and accept the default choice"
		disableLeaklessDefaultValue  = false
		disableLeaklessUsage         = "Disable leakless if you are having issues with it"
		fastPassDefaultValue         = false
		fastPassUsage                = "Use Okta FastPass verification"
		useSystemBrowserDefaultValue = false
		useSystemBrowserUsage        = "Use System Browser"
	)

	flag.StringVar(&profile, "profile", profileDefaultValue, profileUsage)
	flag.StringVar(&profile, "p", profileDefaultValue, profileUsage+" (shorthand)")
	flag.BoolVar(&allProfiles, "all-profiles", allProfilesDefaultValue, allProfilesUsage)
	flag.BoolVar(&allProfiles, "a", allProfilesDefaultValue, allProfilesUsage+" (shorthand)")
	flag.BoolVar(&forceRefresh, "force-refresh", forceRefreshDefaultValue, forceRefreshUsage)
	flag.BoolVar(&forceRefresh, "f", forceRefreshDefaultValue, forceRefreshUsage+" (shorthand)")
	flag.StringVar(&mode, "mode", modeDefaultValue, modeUsage)
	flag.StringVar(&mode, "m", modeDefaultValue, modeUsage+" (shorthand)")
	flag.BoolVar(&configure, "configure", configureDefaultValue, configureUsage)
	flag.BoolVar(&configure, "c", configureDefaultValue, configureUsage+" (shorthand)")
	flag.BoolVar(&noVerifySSL, "no-verify-ssl", noVerifySSLDefaultValue, noVerifySSLUsage)
	flag.BoolVar(&noPrompt, "no-prompt", noPromptDefaultValue, noPromptUsage)
	flag.BoolVar(&disableLeakless, "disable-leakless", disableLeaklessDefaultValue, disableLeaklessUsage)
	flag.BoolVar(&fastPass, "fastpass", fastPassDefaultValue, fastPassUsage)
	flag.BoolVar(&useSystemBrowser, "system-browser", useSystemBrowserDefaultValue, useSystemBrowserUsage)

	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "Error: Unused command line arguments detected.\n")
		flag.Usage()
		os.Exit(2)
	}
}

func main() {
	defer appCancel()

	var profileName string
	isGui := mode == "gui"
	isDebug := mode == "debug"
	showBrowser := mode == "gui" || mode == "debug"

	if profile != "" {
		profileName = profile
	} else if osAWSProfile := os.Getenv("AWS_PROFILE"); osAWSProfile != "" {
		profileName = osAWSProfile
	} else {
		profileName = "default"
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

	if configure {
		configureProfile(profileName)
	} else {
		log.Info().Msg("Press 'q' + Enter to quit")
		done := make(chan struct{})
		go func() {
			if allProfiles {
				loginAll(opts)
			} else {
				login(profileName, opts)
			}
			close(done)
		}()

		// Wait for completion or context cancellation (from signal handler)
		select {
		case <-done:
			// Normal completion
		case <-appCtx.Done():
			// Signal received, exit handled by goroutine
		}
	}
}
