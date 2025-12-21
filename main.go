package main

import (
	"context"
	"os"

	"github.com/ZhuMon/go-aws-azure-login/cmd"
	"github.com/rs/zerolog"
)

var log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

func init() {
	// Set up callback functions for cmd package
	cmd.LoginFunc = func(profile string, opts cmd.LoginOptions) {
		login(profile, convertOptions(opts))
	}
	cmd.LoginAllFunc = func(opts cmd.LoginOptions) {
		loginAll(convertOptions(opts))
	}
	cmd.LoginMultipleFunc = func(profiles []string, opts cmd.LoginOptions) {
		loginMultiple(profiles, convertOptions(opts))
	}
	cmd.ConfigureFunc = configureProfile
}

// convertOptions converts cmd.LoginOptions to main.LoginOptions
func convertOptions(opts cmd.LoginOptions) LoginOptions {
	var ctx context.Context
	if opts.Ctx != nil {
		ctx = opts.Ctx.(context.Context)
	} else {
		ctx = context.Background()
	}
	return LoginOptions{
		Ctx:              ctx,
		NoPrompt:         opts.NoPrompt,
		IsGui:            opts.IsGui,
		IsDebug:          opts.IsDebug,
		ShowBrowser:      opts.ShowBrowser,
		DisableLeakless:  opts.DisableLeakless,
		FastPass:         opts.FastPass,
		UseSystemBrowser: opts.UseSystemBrowser,
		AwsNoVerifySsl:   opts.AwsNoVerifySsl,
		ForceRefresh:     opts.ForceRefresh,
	}
}

func main() {
	cmd.Execute()
}
