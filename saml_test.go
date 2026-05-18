package main

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

// buildSamlAssertion builds a base64-encoded SAML response containing the given
// `Role` AttributeValue strings. Each value is the raw "<role-arn>,<principal-arn>"
// pair expected by parseRolesFromSamlResponse.
func buildSamlAssertion(t *testing.T, roleValues ...string) string {
	t.Helper()

	var sb strings.Builder
	sb.WriteString(`<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol">`)
	sb.WriteString(`<Assertion xmlns="urn:oasis:names:tc:SAML:2.0:assertion">`)
	sb.WriteString(`<AttributeStatement>`)
	sb.WriteString(`<Attribute Name="https://aws.amazon.com/SAML/Attributes/Role">`)
	for _, v := range roleValues {
		sb.WriteString(`<AttributeValue>`)
		sb.WriteString(v)
		sb.WriteString(`</AttributeValue>`)
	}
	sb.WriteString(`</Attribute>`)
	sb.WriteString(`</AttributeStatement>`)
	sb.WriteString(`</Assertion>`)
	sb.WriteString(`</samlp:Response>`)

	return base64.StdEncoding.EncodeToString([]byte(sb.String()))
}

func TestParseRolesFromSamlResponse_RoleFirst(t *testing.T) {
	const roleArn = "arn:aws:iam::111111111111:role/Admin"
	const principalArn = "arn:aws:iam::111111111111:saml-provider/AzureAD"

	roles, err := parseRolesFromSamlResponse(buildSamlAssertion(t, roleArn+","+principalArn))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(roles) != 1 {
		t.Fatalf("expected 1 role, got %d", len(roles))
	}
	if roles[0].roleArn != roleArn || roles[0].principalArn != principalArn {
		t.Errorf("got role=%q principal=%q", roles[0].roleArn, roles[0].principalArn)
	}
}

func TestParseRolesFromSamlResponse_PrincipalFirst(t *testing.T) {
	// Some IdPs emit principal,role (reversed). The parser must detect ":role/"
	// in the second segment and swap the fields.
	const roleArn = "arn:aws:iam::222222222222:role/Reader"
	const principalArn = "arn:aws:iam::222222222222:saml-provider/AzureAD"

	roles, err := parseRolesFromSamlResponse(buildSamlAssertion(t, principalArn+","+roleArn))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(roles) != 1 {
		t.Fatalf("expected 1 role, got %d", len(roles))
	}
	if roles[0].roleArn != roleArn || roles[0].principalArn != principalArn {
		t.Errorf("got role=%q principal=%q", roles[0].roleArn, roles[0].principalArn)
	}
}

func TestParseRolesFromSamlResponse_TrimsWhitespace(t *testing.T) {
	const roleArn = "arn:aws:iam::333333333333:role/Foo"
	const principalArn = "arn:aws:iam::333333333333:saml-provider/AzureAD"

	roles, err := parseRolesFromSamlResponse(buildSamlAssertion(t, roleArn+" ,  "+principalArn))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if roles[0].roleArn != roleArn || roles[0].principalArn != principalArn {
		t.Errorf("expected whitespace-trimmed values, got role=%q principal=%q", roles[0].roleArn, roles[0].principalArn)
	}
}

func TestParseRolesFromSamlResponse_MultipleRoles(t *testing.T) {
	values := []string{
		"arn:aws:iam::111111111111:role/Admin,arn:aws:iam::111111111111:saml-provider/AzureAD",
		"arn:aws:iam::222222222222:role/Reader,arn:aws:iam::222222222222:saml-provider/AzureAD",
	}
	roles, err := parseRolesFromSamlResponse(buildSamlAssertion(t, values...))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("expected 2 roles, got %d", len(roles))
	}
}

func TestParseRolesFromSamlResponse_InvalidBase64(t *testing.T) {
	if _, err := parseRolesFromSamlResponse("not-base64!!"); err == nil {
		t.Fatal("expected error for invalid base64 input")
	}
}

func TestParseRolesFromSamlResponse_InvalidXML(t *testing.T) {
	garbage := base64.StdEncoding.EncodeToString([]byte("<this is not <valid xml>>"))
	if _, err := parseRolesFromSamlResponse(garbage); err == nil {
		t.Fatal("expected error for invalid XML")
	}
}

func TestRoleArns(t *testing.T) {
	got := roleArns([]role{
		{roleArn: "a", principalArn: "p1"},
		{roleArn: "b", principalArn: "p2"},
	})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("got %v", got)
	}
}

func TestAskUserForRoleAndDuration_NoRolesReturnsError(t *testing.T) {
	_, _, err := askUserForRoleAndDuration(nil, true, "", "1")
	if err == nil {
		t.Fatal("expected error when SAML response has no roles")
	}
	if !strings.Contains(err.Error(), "no roles") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestAskUserForRoleAndDuration_SingleRoleAutoSelected(t *testing.T) {
	only := role{roleArn: "arn:aws:iam::1:role/A", principalArn: "arn:aws:iam::1:saml-provider/X"}
	got, dur, err := askUserForRoleAndDuration([]role{only}, true, "", "2")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != only {
		t.Errorf("expected the single role to be auto-selected, got %+v", got)
	}
	if dur != 2 {
		t.Errorf("expected durationHours=2, got %d", dur)
	}
}

func TestAskUserForRoleAndDuration_NoPromptDefaultArnMatches(t *testing.T) {
	r1 := role{roleArn: "arn:aws:iam::1:role/A", principalArn: "p1"}
	r2 := role{roleArn: "arn:aws:iam::1:role/B", principalArn: "p2"}

	got, _, err := askUserForRoleAndDuration([]role{r1, r2}, true, r2.roleArn, "1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != r2 {
		t.Errorf("expected default role to be selected, got %+v", got)
	}
}

func TestAskUserForRoleAndDuration_NoPromptDefaultArnNotInSaml(t *testing.T) {
	r1 := role{roleArn: "arn:aws:iam::1:role/A", principalArn: "p1"}
	r2 := role{roleArn: "arn:aws:iam::1:role/B", principalArn: "p2"}

	_, _, err := askUserForRoleAndDuration([]role{r1, r2}, true, "arn:aws:iam::1:role/Missing", "1")
	if err == nil {
		t.Fatal("expected error when default role ARN does not match any SAML role")
	}
	// Error message must surface the offending value and the available options
	// so the user can pick a real ARN.
	msg := err.Error()
	if !strings.Contains(msg, "Missing") {
		t.Errorf("error should name the missing ARN, got: %v", err)
	}
	if !strings.Contains(msg, "role/A") || !strings.Contains(msg, "role/B") {
		t.Errorf("error should list available ARNs, got: %v", err)
	}
}

func TestAskUserForRoleAndDuration_NoPromptMultipleRolesNoDefault(t *testing.T) {
	r1 := role{roleArn: "arn:aws:iam::1:role/A", principalArn: "p1"}
	r2 := role{roleArn: "arn:aws:iam::1:role/B", principalArn: "p2"}

	_, _, err := askUserForRoleAndDuration([]role{r1, r2}, true, "", "1")
	if err == nil {
		t.Fatal("expected error when noPrompt is set, multiple roles exist, and no default ARN is configured")
	}
	if !strings.Contains(err.Error(), "azure_default_role_arn") {
		t.Errorf("error should mention azure_default_role_arn, got: %v", err)
	}
}

func TestAssumeRole_RejectsEmptyArns(t *testing.T) {
	// Defense-in-depth: assumeRole must refuse before calling STS so a misconfigured
	// profile in batch mode produces a readable error instead of a 400 ValidationError.
	err := assumeRole("anyprofile", "fake-saml", role{}, 1, false, nil)
	if err == nil {
		t.Fatal("expected assumeRole to reject empty role/principal ARNs")
	}
	if !strings.Contains(err.Error(), "empty roleArn") {
		t.Errorf("error should mention the empty-arn condition, got: %v", err)
	}
}

func TestCreateLoginUrl_ContainsExpectedQueryFields(t *testing.T) {
	const tenantID = "11111111-2222-3333-4444-555555555555"
	const appURI = "https://example.com/app"
	const acsURL = "https://signin.aws.amazon.com/saml"

	got := createLoginUrl(appURI, tenantID, acsURL)

	if !strings.HasPrefix(got, "https://login.microsoftonline.com/"+tenantID+"/saml2?SAMLRequest=") {
		t.Fatalf("unexpected login URL: %s", got)
	}

	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	if u.Query().Get("SAMLRequest") == "" {
		t.Errorf("expected non-empty SAMLRequest query param")
	}
}
