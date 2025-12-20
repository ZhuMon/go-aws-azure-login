package main

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/go-rod/rod"
)

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

func login(profileName string, opts LoginOptions) {
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

	saml := performLogin(loginUrl, opts.NoPrompt, profile.AzureDefaultUsername, profile.AzureDefaultPassword, profile.OktaDefaultUsername, profile.OktaDefaultPassword, opts.IsGui, opts.ShowBrowser, opts.DisableLeakless, opts.FastPass, opts.UseSystemBrowser)

	roles := parseRolesFromSamlResponse(saml)

	rl, durationHours := askUserForRoleAndDuration(roles, opts.NoPrompt, profile.AzureDefaultRoleArn, profile.AzureDefaultDurationHours)

	assumeRole(profileName, saml, rl, durationHours, opts.AwsNoVerifySsl, profile.Region)
}

func loginAll(opts LoginOptions) {
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
		if !opts.ForceRefresh && !isProfileAboutToExpire(profileName) {
			continue
		}
		profilesToLogin = append(profilesToLogin, profileName)
	}

	if len(profilesToLogin) == 0 {
		fmt.Println("No profiles need refresh")
		return
	}

	// Create browser once and reuse for all profiles
	browser, cleanup := createBrowser(opts.ShowBrowser, opts.DisableLeakless, opts.UseSystemBrowser)
	defer cleanup()

	for i, profileName := range profilesToLogin {
		fmt.Printf("\n[%d/%d] Logging in profile: %s\n", i+1, len(profilesToLogin), profileName)
		loginWithBrowser(browser, profileName, opts)
	}
}

func loginWithBrowser(browser *rod.Browser, profileName string, opts LoginOptions) {
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

	saml := performLoginWithBrowser(browser, loginUrl, opts.NoPrompt, profile.AzureDefaultUsername, profile.AzureDefaultPassword, profile.OktaDefaultUsername, profile.OktaDefaultPassword, opts.IsGui, opts.FastPass)

	roles := parseRolesFromSamlResponse(saml)

	rl, durationHours := askUserForRoleAndDuration(roles, opts.NoPrompt, profile.AzureDefaultRoleArn, profile.AzureDefaultDurationHours)

	assumeRole(profileName, saml, rl, durationHours, opts.AwsNoVerifySsl, profile.Region)
}
