package main

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"

	"github.com/ZhuMon/go-aws-azure-login/cmd"
	"github.com/go-rod/rod"
)

// profileResult records the outcome of a single profile in a batch login.
type profileResult struct {
	name string
	err  error
}

const (
	AWS_SAML_ENDPOINT     = "https://signin.aws.amazon.com/saml"
	AWS_CN_SAML_ENDPOINT  = "https://signin.amazonaws.cn/saml"
	AWS_GOV_SAML_ENDPOINT = "https://signin.amazonaws-us-gov.com/saml"
	OKTA_SELECT_FAST_PASS = "OKTA SELECT FastPass"
	OKTA_SELECT_PUSH_FORM = "OKTA SELECT PUSH Form"
	OKTA_DO_PUSH_FORM     = "OKTA DO PUSH Form"

	WIDTH  = 425
	HEIGHT = 550
)

func loadProfileFromEnv() profileConfig {
	envVars := []string{
		"azure_tenant_id",
		"azure_app_id_uri",
		"azure_default_username",
		"azure_default_password",
		"azure_default_role_arn",
		"azure_default_duration_hours",
		"region",
		"okta_default_username",
		"okta_default_password",
	}

	profile := profileConfig{}

	v := reflect.ValueOf(&profile).Elem()
	t := v.Type()

	for _, envVar := range envVars {
		val, exists := os.LookupEnv(envVar)
		if exists {
			for i := 0; i < v.NumField(); i++ {
				tag := t.Field(i).Tag.Get(tagName)
				if tag == envVar {
					f := v.Field(i)
					if f.Kind() == reflect.String {
						f.SetString(val)
					} else if f.Kind() == reflect.Ptr {
						f.Set(reflect.ValueOf(&val))
					}
				}
			}
		}
	}

	return profile
}

func loadProfile(profileName string) profileConfig {
	profile := getProfileConfig(profileName)
	envProfile := loadProfileFromEnv()

	if (envProfile != profileConfig{}) {
		v := reflect.ValueOf(&profile).Elem()
		vEnv := reflect.ValueOf(&envProfile).Elem()
		t := v.Type()

		for i := 0; i < t.NumField(); i++ {
			value := v.Field(i)
			envValue := vEnv.Field(i)

			if envValue.Kind() == reflect.Ptr {
				if !envValue.IsNil() {
					value.Set(reflect.ValueOf(envValue.Interface()))
				}
			} else if envValue.Kind() == reflect.Bool {
				if value.Interface().(bool) != envValue.Interface().(bool) {
					value.SetBool(envValue.Interface().(bool))
				}
			} else {
				nValue := envValue.Interface().(string)
				if value.Interface().(string) != nValue && nValue != "" {
					value.SetString(envValue.Interface().(string))
				}
			}
		}
	}

	return profile
}

func login(profileName string, opts LoginOptions) error {
	// Check if credentials are still valid (unless force refresh is requested)
	expiring, err := isProfileAboutToExpire(profileName)
	if err != nil {
		return err
	}
	if !opts.ForceRefresh && !expiring {
		log.Info().Str("profile", profileName).Msg("Credentials still valid, skipping refresh")
		return nil
	}

	log.Info().Str("profile", profileName).Msg("Starting login")

	profile := loadProfile(profileName)

	assertionConsumerServiceURL := AWS_SAML_ENDPOINT

	if profile.Region != nil {
		if strings.HasPrefix(*profile.Region, "us-gov") {
			assertionConsumerServiceURL = AWS_GOV_SAML_ENDPOINT
		} else if strings.HasPrefix(*profile.Region, "cn-") {
			assertionConsumerServiceURL = AWS_CN_SAML_ENDPOINT
		}
	}

	loginUrl := createLoginUrl(profile.AzureAppIDUri, profile.AzureTenantID, assertionConsumerServiceURL)

	saml, err := performLogin(opts.Ctx, loginUrl, opts.NoPrompt, profile.AzureDefaultUsername, profile.AzureDefaultPassword, profile.OktaDefaultUsername, profile.OktaDefaultPassword, opts.IsGui, opts.IsDebug, opts.ShowBrowser, opts.DisableLeakless, opts.FastPass, opts.UseSystemBrowser)
	if err != nil {
		return err
	}

	if saml == "" {
		log.Info().Str("profile", profileName).Msg("Login cancelled")
		return nil
	}

	log.Info().Msg("SAML response received, parsing roles")

	roles, err := parseRolesFromSamlResponse(saml)
	if err != nil {
		return err
	}

	log.Info().Int("count", len(roles)).Msg("Roles found")

	rl, durationHours, err := askUserForRoleAndDuration(roles, opts.NoPrompt, profile.AzureDefaultRoleArn, profile.AzureDefaultDurationHours)
	if err != nil {
		return err
	}

	if err := assumeRole(profileName, saml, rl, durationHours, opts.AwsNoVerifySsl, profile.Region); err != nil {
		return err
	}

	log.Info().Str("profile", profileName).Msg("Login successful")
	return nil
}

func loginAll(opts LoginOptions) error {
	allProfiles := getAllProfileNames()

	// Filter profiles that need refresh and are properly configured
	var profilesToLogin []string
	for _, profileName := range allProfiles {
		profile := getProfileConfig(profileName)
		// Skip profiles without azure_tenant_id configured
		if profile.AzureTenantID == "" {
			log.Debug().Str("profile", profileName).Msg("Skipping profile without azure_tenant_id")
			continue
		}
		expiring, err := isProfileAboutToExpire(profileName)
		if err != nil {
			log.Warn().Err(err).Str("profile", profileName).Msg("Skipping profile with invalid expiration")
			continue
		}
		if !opts.ForceRefresh && !expiring {
			continue
		}
		profilesToLogin = append(profilesToLogin, profileName)
	}

	return runBatchLogin(profilesToLogin, opts)
}

// loginMultiple logs in to a specific list of profiles
func loginMultiple(profileNames []string, opts LoginOptions) error {
	// Filter profiles that need refresh
	var profilesToLogin []string
	for _, profileName := range profileNames {
		profile := getProfileConfig(profileName)
		// Skip profiles without azure_tenant_id configured
		if profile.AzureTenantID == "" {
			log.Warn().Str("profile", profileName).Msg("Skipping profile without azure_tenant_id")
			continue
		}
		expiring, err := isProfileAboutToExpire(profileName)
		if err != nil {
			log.Warn().Err(err).Str("profile", profileName).Msg("Skipping profile with invalid expiration")
			continue
		}
		if !opts.ForceRefresh && !expiring {
			log.Debug().Str("profile", profileName).Msg("Skipping profile - credentials still valid")
			continue
		}
		profilesToLogin = append(profilesToLogin, profileName)
	}

	return runBatchLogin(profilesToLogin, opts)
}

// runBatchLogin processes a list of profiles sequentially using a single shared browser.
// When opts.ContinueOnError is true, per-profile failures are recorded and the loop
// continues; otherwise the loop aborts on first failure. A summary is printed in both
// modes whenever more than one profile was attempted, and an aggregate error is
// returned when any profile failed.
func runBatchLogin(profilesToLogin []string, opts LoginOptions) error {
	if len(profilesToLogin) == 0 {
		log.Info().Msg("No profiles need refresh")
		return nil
	}

	// Create browser once and reuse for all profiles
	browser, cleanup := createBrowser(opts.Ctx, opts.ShowBrowser, opts.DisableLeakless, opts.UseSystemBrowser)

	// The CLI exits via os.Exit on the signal / 'q' paths, which skips defers.
	// Register the cleanup so those paths kill the browser before exiting;
	// sync.Once keeps the defer and the exit hook from closing it twice.
	var once sync.Once
	safeCleanup := func() { once.Do(cleanup) }
	cmd.RegisterCleanup(safeCleanup)
	defer safeCleanup()

	if browser == nil {
		return nil
	}

	results := runProfileLoop(profilesToLogin, opts.ContinueOnError, opts.Ctx.Done(), func(profileName string) error {
		return runProfileSafely(browser, profileName, opts)
	})

	return finalizeBatch(results, len(profilesToLogin))
}

// runProfileLoop runs `step` once per profile, recording the result. When
// continueOnError is false the loop aborts on the first non-nil error.
// When the cancel channel signals, the loop exits at the next iteration boundary.
// Extracted so the loop control flow is unit-testable independently of the browser.
func runProfileLoop(profiles []string, continueOnError bool, cancel <-chan struct{}, step func(string) error) []profileResult {
	results := make([]profileResult, 0, len(profiles))
	for i, profileName := range profiles {
		select {
		case <-cancel:
			return results
		default:
		}

		log.Info().Int("current", i+1).Int("total", len(profiles)).Str("profile", profileName).Msg("Logging in profile")

		err := step(profileName)
		results = append(results, profileResult{name: profileName, err: err})

		if err != nil {
			log.Error().Err(err).Str("profile", profileName).Msg("Profile login failed")
			if !continueOnError {
				return results
			}
		}
	}
	return results
}

// runProfileSafely runs loginWithBrowser and converts panics to errors so a panic
// in one profile cannot kill the rest of the batch.
func runProfileSafely(browser *rod.Browser, profileName string, opts LoginOptions) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic during login: %v", r)
		}
	}()
	return loginWithBrowser(browser, profileName, opts)
}

// finalizeBatch prints the summary and returns an aggregate error if any profile failed.
func finalizeBatch(results []profileResult, totalAttempted int) error {
	succeeded, failed := 0, 0
	for _, r := range results {
		if r.err == nil {
			succeeded++
		} else {
			failed++
		}
	}

	// Don't print a summary when only one profile was processed — keep single-profile output unchanged.
	if totalAttempted > 1 || failed > 0 {
		fmt.Fprintf(os.Stderr, "\nBatch summary: %d succeeded, %d failed (%d total)\n", succeeded, failed, len(results))
		for _, r := range results {
			if r.err != nil {
				fmt.Fprintf(os.Stderr, "  FAILED %s: %v\n", r.name, r.err)
			}
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d profile(s) failed", failed)
	}
	return nil
}

func loginWithBrowser(browser *rod.Browser, profileName string, opts LoginOptions) error {
	profile := loadProfile(profileName)

	assertionConsumerServiceURL := AWS_SAML_ENDPOINT

	if profile.Region != nil {
		if strings.HasPrefix(*profile.Region, "us-gov") {
			assertionConsumerServiceURL = AWS_GOV_SAML_ENDPOINT
		} else if strings.HasPrefix(*profile.Region, "cn-") {
			assertionConsumerServiceURL = AWS_CN_SAML_ENDPOINT
		}
	}

	loginUrl := createLoginUrl(profile.AzureAppIDUri, profile.AzureTenantID, assertionConsumerServiceURL)

	saml, err := performLoginWithBrowser(opts.Ctx, browser, loginUrl, opts.NoPrompt, profile.AzureDefaultUsername, profile.AzureDefaultPassword, profile.OktaDefaultUsername, profile.OktaDefaultPassword, opts.IsGui, opts.IsDebug, opts.FastPass)
	if err != nil {
		return err
	}

	if saml == "" {
		// Empty SAML means the user closed the window or cancelled — preserved
		// as a no-op skip to match historical single-profile behavior.
		log.Info().Str("profile", profileName).Msg("Login cancelled")
		return nil
	}

	roles, err := parseRolesFromSamlResponse(saml)
	if err != nil {
		return err
	}

	rl, durationHours, err := askUserForRoleAndDuration(roles, opts.NoPrompt, profile.AzureDefaultRoleArn, profile.AzureDefaultDurationHours)
	if err != nil {
		return err
	}

	if err := assumeRole(profileName, saml, rl, durationHours, opts.AwsNoVerifySsl, profile.Region); err != nil {
		return err
	}

	log.Info().Str("profile", profileName).Msg("Login successful")
	return nil
}
