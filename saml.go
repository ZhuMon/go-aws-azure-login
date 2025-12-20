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
	"os"
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

	samlRequest := fmt.Sprintf(`
	<samlp:AuthnRequest xmlns="urn:oasis:names:tc:SAML:2.0:metadata" ID="id%s" Version="2.0" IssueInstant="%s" IsPassive="false" AssertionConsumerServiceURL="%s" xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol">
		<Issuer xmlns="urn:oasis:names:tc:SAML:2.0:assertion">%s</Issuer>
		<samlp:NameIDPolicy Format="urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress"></samlp:NameIDPolicy>
	</samlp:AuthnRequest>
	`, id, time.Now().Format(time.RFC3339), assertionConsumerServiceURL, appIDUri)

	var buffer bytes.Buffer

	flateWriter, _ := flate.NewWriter(&buffer, -1)

	flateWriter.Write([]byte(samlRequest))
	flateWriter.Flush()
	flateWriter.Close()

	samlBase64 := base64.StdEncoding.EncodeToString(buffer.Bytes())

	return fmt.Sprintf("https://login.microsoftonline.com/%s/saml2?SAMLRequest=%s", tenantID, url.QueryEscape(samlBase64))
}

func parseRolesFromSamlResponse(assertion string) []role {
	b64, err := base64.StdEncoding.DecodeString(assertion)

	if err != nil {
		fmt.Printf("Fail to parse roles: %v", err)
		os.Exit(1)
	}

	var roles []role
	var sResponse samlResponse

	err = xml.Unmarshal(b64, &sResponse)

	if err != nil {
		fmt.Printf("Fail to unmarshal roles: %v", err)
		os.Exit(1)
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

	return roles
}

func askUserForRoleAndDuration(
	roles []role,
	noPrompt bool,
	defaultRoleArn string,
	defaultDurationHours string) (r role, durationHours int32) {
	durationHoursP, _ := strconv.ParseInt(defaultDurationHours, 10, 32)
	durationHours = int32(durationHoursP)

	if len(roles) == 0 {
		fmt.Println("No roles found in SAML response.")
		os.Exit(1)
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
		}

		if (role{} == r) {
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
			survey.AskOne(prompt, &rArn, survey.WithValidator(survey.Required))

			for _, rl := range roles {
				if rl.roleArn == rArn {
					r = rl
					break
				}
			}
		}
	}

	if !(noPrompt && defaultDurationHours != "") {
		inp := &survey.Input{Message: "Session Duration Hours (up to 12):", Default: defaultDurationHours}
		hq := ""

		survey.AskOne(inp, &hq, survey.WithValidator(func(val interface{}) error {
			if str, ok := val.(string); !ok {
				return errors.New("invalid number")
			} else if n, err := strconv.ParseInt(str, 10, 32); err != nil || n <= 0 || n > 12 {
				return errors.New("duration hours must be between 0 and 12")
			}
			return nil
		}))

		durationHoursP, _ = strconv.ParseInt(hq, 10, 32)
		durationHours = int32(durationHoursP)
	}
	return
}

func assumeRole(
	profileName string,
	assertion string,
	role role,
	durationHours int32,
	awsNoVerifySsl bool,
	region *string) {

	durationSeconds := durationHours * 60 * 60
	stsInput := sts.AssumeRoleWithSAMLInput{
		PrincipalArn:    &role.principalArn,
		RoleArn:         &role.roleArn,
		SAMLAssertion:   &assertion,
		DurationSeconds: &durationSeconds,
	}

	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		fmt.Printf("Fail to get AWS config: %v", err)
		os.Exit(1)
	}

	if region != nil {
		cfg.Region = *region
	}

	stsClient := sts.NewFromConfig(cfg)

	stsResult, err := stsClient.AssumeRoleWithSAML(context.Background(), &stsInput)

	if err != nil {
		fmt.Printf("Fail to assume role: %v", err)
		os.Exit(1)
	}

	setProfileCredentials(profileName,
		profileCredentials{
			AwsAccessKeyID:     *stsResult.Credentials.AccessKeyId,
			AwsSecretAccessKey: *stsResult.Credentials.SecretAccessKey,
			AwsSessionToken:    *stsResult.Credentials.SessionToken,
			AwsExpiration:      (*stsResult.Credentials.Expiration).Format(timeFormat),
		},
	)
}
