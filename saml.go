package main

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/google/uuid"
)

func createLoginUrl(appIDUri string, tenantID string, assertionConsumerServiceURL string) string {
	id := uuid.NewString()

	samlRequest := `
	<samlp:AuthnRequest xmlns="urn:oasis:names:tc:SAML:2.0:metadata" ID="id` + id + `" Version="2.0" IssueInstant="` + time.Now().Format(time.RFC3339) + `" IsPassive="false" AssertionConsumerServiceURL="` + assertionConsumerServiceURL + `" xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol">
		<Issuer xmlns="urn:oasis:names:tc:SAML:2.0:assertion">` + appIDUri + `</Issuer>
		<samlp:NameIDPolicy Format="urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress"></samlp:NameIDPolicy>
	</samlp:AuthnRequest>
	`

	var buffer bytes.Buffer

	flateWriter, err := flate.NewWriter(&buffer, flate.DefaultCompression)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create flate writer")
	}

	if _, err := flateWriter.Write([]byte(samlRequest)); err != nil {
		log.Fatal().Err(err).Msg("Failed to write SAML request")
	}
	if err := flateWriter.Flush(); err != nil {
		log.Fatal().Err(err).Msg("Failed to flush flate writer")
	}
	if err := flateWriter.Close(); err != nil {
		log.Fatal().Err(err).Msg("Failed to close flate writer")
	}

	samlBase64 := base64.StdEncoding.EncodeToString(buffer.Bytes())

	return "https://login.microsoftonline.com/" + tenantID + "/saml2?SAMLRequest=" + url.QueryEscape(samlBase64)
}

func roleArns(roles []role) []string {
	arns := make([]string, len(roles))
	for i, r := range roles {
		arns[i] = r.roleArn
	}
	return arns
}

func parseRolesFromSamlResponse(assertion string) ([]role, error) {
	b64, err := base64.StdEncoding.DecodeString(assertion)
	if err != nil {
		return nil, fmt.Errorf("decode SAML response: %w", err)
	}

	var roles []role
	var sResponse samlResponse

	err = xml.Unmarshal(b64, &sResponse)
	if err != nil {
		return nil, fmt.Errorf("unmarshal SAML response: %w", err)
	}

	for _, attr := range sResponse.Assertion.AttributeStatement.Attributes {
		if attr.Name == "https://aws.amazon.com/SAML/Attributes/Role" {
			for _, val := range attr.AttributeValues {
				parts := strings.Split(val.Value, ",")

				if strings.Contains(parts[0], ":role/") {
					roles = append(roles, role{
						roleArn:      strings.TrimSpace(parts[0]),
						principalArn: strings.TrimSpace(parts[1]),
					})
				} else {
					roles = append(roles, role{
						roleArn:      strings.TrimSpace(parts[1]),
						principalArn: strings.TrimSpace(parts[0]),
					})
				}

			}
		}
	}

	return roles, nil
}

func askUserForRoleAndDuration(
	roles []role,
	noPrompt bool,
	defaultRoleArn string,
	defaultDurationHours string) (r role, durationHours int32, err error) {
	durationHoursP, perr := strconv.ParseInt(defaultDurationHours, 10, 32)
	if perr != nil && defaultDurationHours != "" {
		log.Warn().Err(perr).Str("value", defaultDurationHours).Msg("Invalid default duration hours, using 0")
	}
	durationHours = int32(durationHoursP)

	if len(roles) == 0 {
		err = errors.New("no roles found in SAML response")
		return
	} else if len(roles) == 1 {
		r = roles[0]
	} else {
		if noPrompt && defaultRoleArn != "" {
			for _, rl := range roles {
				if rl.roleArn == defaultRoleArn {
					r = rl
					break
				}
			}
			if (role{} == r) {
				err = fmt.Errorf("azure_default_role_arn %q not found in SAML response (available: %v)", defaultRoleArn, roleArns(roles))
				return
			}
		}

		if (role{} == r) {
			if noPrompt {
				err = fmt.Errorf("multiple roles in SAML response and azure_default_role_arn is not configured (available: %v)", roleArns(roles))
				return
			}

			var options []string

			for _, rl := range roles {
				options = append(options, rl.roleArn)
			}

			rArn := ""
			prompt := &survey.Select{
				Message: "Role:",
				Options: options,
				Default: defaultRoleArn,
			}
			if perr := survey.AskOne(prompt, &rArn, survey.WithValidator(survey.Required)); perr != nil {
				err = fmt.Errorf("role selection: %w", perr)
				return
			}

			for _, rl := range roles {
				if rl.roleArn == rArn {
					r = rl
					break
				}
			}
			if (role{} == r) {
				err = fmt.Errorf("selected role %q not found in SAML response", rArn)
				return
			}
		}
	}

	if !(noPrompt && defaultDurationHours != "") {
		inputDefault := defaultDurationHours
		if inputDefault == "" {
			inputDefault = "1"
		}
		inp := &survey.Input{Message: "Session Duration Hours (up to 12):", Default: inputDefault}
		hq := ""

		survey.AskOne(inp, &hq, survey.WithValidator(func(val interface{}) error {
			if str, ok := val.(string); !ok {
				return errors.New("invalid number")
			} else if n, err := strconv.ParseInt(str, 10, 32); err != nil || n <= 0 || n > 12 {
				return errors.New("duration hours must be between 0 and 12")
			}
			return nil
		}))

		parsed, perr := strconv.ParseInt(hq, 10, 32)
		if perr != nil {
			log.Warn().Err(perr).Str("value", hq).Msg("Invalid duration hours input, using default")
		} else {
			durationHours = int32(parsed)
		}
	}
	return
}

func assumeRole(
	profileName string,
	assertion string,
	role role,
	durationHours int32,
	awsNoVerifySsl bool,
	region *string) error {

	if role.roleArn == "" || role.principalArn == "" {
		return fmt.Errorf("refusing to assume role with empty roleArn/principalArn (likely a misconfigured azure_default_role_arn)")
	}

	durationSeconds := durationHours * 60 * 60
	stsInput := sts.AssumeRoleWithSAMLInput{
		PrincipalArn:    &role.principalArn,
		RoleArn:         &role.roleArn,
		SAMLAssertion:   &assertion,
		DurationSeconds: &durationSeconds,
	}

	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	if region != nil {
		cfg.Region = *region
	}

	stsClient := sts.NewFromConfig(cfg)

	stsResult, err := stsClient.AssumeRoleWithSAML(context.Background(), &stsInput)
	if err != nil {
		return fmt.Errorf("assume role %s: %w", role.roleArn, err)
	}

	setProfileCredentials(profileName,
		profileCredentials{
			AwsAccessKeyID:     *stsResult.Credentials.AccessKeyId,
			AwsSecretAccessKey: *stsResult.Credentials.SecretAccessKey,
			AwsSessionToken:    *stsResult.Credentials.SessionToken,
			AwsExpiration:      (*stsResult.Credentials.Expiration).Format(timeFormat),
		},
	)
	return nil
}
