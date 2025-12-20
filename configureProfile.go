package main

import (
	"errors"
	"strconv"

	"github.com/AlecAivazis/survey/v2"
)

func configureProfile(profileName string) {
	// Ask for profile name first
	promptedName := ""
	err := survey.AskOne(&survey.Input{
		Message: "Profile name:",
		Default: profileName,
	}, &promptedName)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to get profile name")
	}
	if promptedName != "" {
		profileName = promptedName
	}

	profile := getProfileConfig(profileName)

	// Use intermediate struct to avoid survey's pointer type issues
	answers := struct {
		TenantID             string `survey:"tenantId"`
		AppIDUri             string `survey:"appIdUri"`
		Username             string `survey:"username"`
		OktaUsername         string `survey:"oktaUsername"`
		RememberMe           bool   `survey:"rememberMe"`
		DefaultRoleArn       string `survey:"defaultRoleArn"`
		DefaultDurationHours string `survey:"defaultDurationHours"`
	}{}

	var qs = []*survey.Question{
		{
			Name:     "tenantId",
			Prompt:   &survey.Input{Message: "Azure Tenant ID:", Default: profile.AzureTenantID},
			Validate: survey.Required,
		},
		{
			Name:     "appIdUri",
			Prompt:   &survey.Input{Message: "Azure App ID URI:", Default: profile.AzureAppIDUri},
			Validate: survey.Required,
		},
		{
			Name:   "username",
			Prompt: &survey.Input{Message: "Default Azure Username:", Default: profile.AzureDefaultUsername},
		},
		{
			Name:   "oktaUsername",
			Prompt: &survey.Input{Message: "Default Okta Username (leave empty if not using Okta):", Default: stringPointerToString(profile.OktaDefaultUsername)},
		},
		{
			Name:   "rememberMe",
			Prompt: &survey.Confirm{Message: "Stay logged in: skip authentication while refreshing aws credentials", Default: profile.AzureDefaultRememberMe},
		},
		{
			Name:   "defaultRoleArn",
			Prompt: &survey.Input{Message: "Default Role ARN (if multiple):", Default: profile.AzureDefaultRoleArn},
		},
		{
			Name:   "defaultDurationHours",
			Prompt: &survey.Input{Message: "Default Session Duration Hours (up to 12):", Default: profile.AzureDefaultDurationHours},
			Validate: func(val interface{}) error {
				str, ok := val.(string)
				if !ok {
					return errors.New("invalid input")
				}
				if str == "" {
					return nil // Allow empty for default
				}
				n, err := strconv.ParseInt(str, 10, 64)
				if err != nil || n <= 0 || n > 12 {
					return errors.New("duration hours must be between 1 and 12")
				}
				return nil
			},
		},
	}

	err = survey.Ask(qs, &answers)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to get profile configuration answers")
	}

	// Map answers to profile
	profile.AzureTenantID = answers.TenantID
	profile.AzureAppIDUri = answers.AppIDUri
	profile.AzureDefaultUsername = answers.Username
	profile.AzureDefaultRememberMe = answers.RememberMe
	profile.AzureDefaultRoleArn = answers.DefaultRoleArn
	profile.AzureDefaultDurationHours = answers.DefaultDurationHours
	if answers.OktaUsername != "" {
		profile.OktaDefaultUsername = &answers.OktaUsername
	} else {
		profile.OktaDefaultUsername = nil
	}

	setProfileConfig(profileName, profile)
}

func stringPointerToString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
