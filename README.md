# go-aws-azure-login

A command-line tool for logging into AWS using Azure Active Directory SSO authentication.

> **Note**: This is a fork of [luneo7/go-aws-azure-login](https://github.com/luneo7/go-aws-azure-login).

## Overview

If your organization uses [Azure Active Directory](https://azure.microsoft.com) for SSO login to the AWS console, this tool lets you authenticate from the command line. It handles the full Azure AD login flow (including MFA) and stores temporary AWS credentials for use with the AWS CLI and SDKs.

## Installation

Download the binary from the [releases page](https://github.com/ZhuMon/go-aws-azure-login/releases), or build from source:

```bash
go install github.com/ZhuMon/go-aws-azure-login@latest
```

## Quick Start

1. **Configure a profile:**
   ```bash
   go-aws-azure-login -configure
   ```

2. **Log in:**
   ```bash
   go-aws-azure-login
   ```

3. **Use AWS CLI as usual:**
   ```bash
   aws s3 ls
   ```

## Configuration

### Basic Setup

Run the configuration wizard:

```bash
# Configure the default profile
go-aws-azure-login -configure

# Configure a named profile
go-aws-azure-login -configure -profile myprofile
```

You'll need:
- **Azure Tenant ID** - Your organization's Azure AD tenant identifier
- **App ID URI** - The application ID URI for the AWS app in Azure AD

See [Getting Your Tenant ID and App ID URI](#getting-your-tenant-id-and-app-id-uri) for help finding these values.

### Environment Variables

You can set these environment variables to skip prompts:

| Variable | Description |
|----------|-------------|
| `AZURE_TENANT_ID` | Azure AD tenant ID |
| `AZURE_APP_ID_URI` | Application ID URI |
| `AZURE_DEFAULT_USERNAME` | Login username |
| `AZURE_DEFAULT_PASSWORD` | Login password |
| `AZURE_DEFAULT_ROLE_ARN` | AWS role ARN to assume |
| `AZURE_DEFAULT_DURATION_HOURS` | Session duration in hours |

For Okta federated logins (untested):

| Variable | Description |
|----------|-------------|
| `OKTA_DEFAULT_USERNAME` | Okta username (if different from Azure) |
| `OKTA_DEFAULT_PASSWORD` | Okta password (if different from Azure) |

> **Note**: Okta federation support is **untested** in this fork.

**Security tip**: Use `HISTCONTROL=ignoreboth` and prefix commands with a space to avoid storing passwords in shell history:

```bash
HISTCONTROL=ignoreboth
 export AZURE_DEFAULT_PASSWORD=mypassword  # Note the leading space
go-aws-azure-login -no-prompt
```

### Stay Logged In

During configuration, you can enable session persistence:

```
? Stay logged in: skip authentication while refreshing aws credentials (true|false)
```

When enabled, subsequent logins reuse session cookies to skip the username/password prompts:

```bash
go-aws-azure-login -no-prompt
```

> **Important**: This feature will **not work** if your organization's IT policy requires MFA verification on every login. In that case, you'll still need to complete MFA each time regardless of this setting.

## Usage

### Basic Commands

```bash
# Login with default profile
go-aws-azure-login

# Login with a named profile
go-aws-azure-login -profile myprofile

# Use AWS_PROFILE environment variable
AWS_PROFILE=myprofile go-aws-azure-login

# Skip prompts (uses saved/environment credentials)
go-aws-azure-login -no-prompt

# Login all configured profiles
go-aws-azure-login -all-profiles

# Force credential refresh (even if not expired)
go-aws-azure-login -force-refresh
```

> **Note**: The tool automatically skips login if credentials are still valid. You'll see status messages like:
> - `INF Credentials still valid, skipping refresh profile=myprofile`
> - `INF Login successful profile=myprofile`
>
> Use `-force-refresh` to force a new login even when credentials haven't expired.

### Display Modes

```bash
# GUI mode (default) - visible browser window for login
go-aws-azure-login -mode gui

# CLI mode - headless browser, prompts in terminal
go-aws-azure-login -mode cli

# Debug mode - visible browser with CLI prompts (for troubleshooting)
go-aws-azure-login -mode debug
```

> **MFA Compatibility**: If your MFA requires viewing a number on screen (e.g., Microsoft Authenticator number matching), **do not use CLI mode**. The headless browser hides the screen, making it impossible to see the verification code. Use GUI mode instead.

### All Options

| Flag | Short | Description |
|------|-------|-------------|
| `-profile` | `-p` | Profile name to use |
| `-all-profiles` | `-a` | Login all configured profiles |
| `-force-refresh` | `-f` | Force credential refresh |
| `-configure` | `-c` | Run configuration wizard |
| `-mode` | `-m` | Display mode: `gui` (default), `cli`, or `debug` |
| `-no-prompt` | | Skip interactive prompts (default: true) |
| `-no-verify-ssl` | | Disable SSL verification for AWS |
| `-disable-leakless` | | Disable leakless mode (troubleshooting) |
| `-fastpass` | | Use Okta FastPass verification (untested) |
| `-system-browser` | | Use system browser instead of embedded |

## Automation

### Refresh All Profiles

Useful for keeping credentials fresh with a cron job:

```bash
# Refresh all profiles without prompts
go-aws-azure-login -all-profiles -no-prompt
```

Credentials are only refreshed if they expire within 11 minutes, so running this frequently is safe.

Example cron entry (every 5 minutes):

```cron
*/5 * * * * /path/to/go-aws-azure-login -all-profiles -no-prompt
```

> **Note**: This only works reliably if your organization allows session persistence. If MFA is required each login, automation is not possible.

## Getting Your Tenant ID and App ID URI

Contact your Azure AD administrator for these values. If unavailable, you can extract them:

1. Go to [myapps.microsoft.com](https://myapps.microsoft.com)
2. Click the AWS app tile
3. Quickly copy the URL from the popup (format: `login.microsoftonline.com/<tenant-id>/...`)
4. The GUID after `login.microsoftonline.com/` is your **Tenant ID**
5. Copy the `SAMLRequest` URL parameter
6. Decode the URL encoding using a [URL decoder](https://www.samltool.com/url.php)
7. Decode the SAML using a [SAML decoder](https://www.samltool.com/decode.php)
8. The `Issuer` value in the decoded XML is your **App ID URI**

## Regional Support

> **Note**: GovCloud and China region support is **untested** in this fork.

To use with AWS GovCloud or China regions, set the `region` in your `~/.aws/config`:

**GovCloud:**
```ini
[profile govcloud]
region = us-gov-west-1
# or us-gov-east-1
```

**China:**
```ini
[profile china]
region = cn-north-1
```

## How It Works

1. **Browser automation**: Uses [Rod](https://github.com/go-rod/rod) to automate a Chromium browser
2. **Azure AD login**: Navigates the Azure login flow, handling credentials and MFA
3. **SAML parsing**: Extracts the SAML assertion from the Azure response
4. **AWS STS**: Calls [AssumeRoleWithSAML](https://docs.aws.amazon.com/STS/latest/APIReference/API_AssumeRoleWithSAML.html) to get temporary credentials
5. **Credential storage**: Saves credentials to `~/.aws/credentials`

## Troubleshooting

### Browser issues

Try these flags:
- `-mode debug` - See what's happening in the browser
- `-disable-leakless` - If you see zombie browser processes
- `-system-browser` - Use your installed browser instead of embedded

### MFA not working

Use `-mode gui` to complete MFA in a visible browser window.

### SSL errors

Use `-no-verify-ssl` if you're behind a corporate proxy with SSL inspection.

## License

See [LICENSE](LICENSE) file.

## Acknowledgments

- Original project: [luneo7/go-aws-azure-login](https://github.com/luneo7/go-aws-azure-login)
- Browser automation: [go-rod/rod](https://github.com/go-rod/rod)
