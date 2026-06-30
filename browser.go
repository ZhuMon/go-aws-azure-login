package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// statePollInterval throttles the state-detection loop so a page that stays on
// the same state (e.g. waiting for MFA) does not flood Chromium with CDP calls.
const statePollInterval = 200 * time.Millisecond

// chromiumRevision pins the auto-downloaded Chromium build. rod v0.116.2 ships
// with revision 1321438 (Chromium 128), which crashes under macOS's newer
// graphics/sandbox stack when the login page is automated. This newer revision
// is compatible.
const chromiumRevision = 1654715

func createBrowser(ctx context.Context, showBrowser bool, disableLeakless bool, useSystemBrowser bool) (*rod.Browser, func()) {
	l := launcher.New().Headless(!showBrowser).UserDataDir(paths[CHROMIUM]).Context(ctx)
	log.Debug().
		Bool("showBrowser", showBrowser).
		Bool("disableLeakless", disableLeakless).
		Bool("useSystemBrowser", useSystemBrowser).
		Str("userDataDir", paths[CHROMIUM]).
		Msg("Creating browser launcher")

	if useSystemBrowser {
		if path, exists := launcher.LookPath(); exists {
			log.Info().Str("path", path).Msg("Using system browser")
			l.Bin(path)
		} else {
			log.Warn().Msg("No usable system browser found, falling back to own Chromium browser")
			l.Revision(chromiumRevision)
		}
	} else {
		l.Revision(chromiumRevision)
	}

	l.Leakless(!disableLeakless)
	l.Set("window-size", fmt.Sprintf("%d,%d", WIDTH, HEIGHT))

	// The CLI exits via os.Exit (signal handler / 'q' / success path), so the
	// browser is never closed cleanly and Chromium records exit_type "Crashed".
	// On the next launch it restores the previous tabs, so pages pile up run
	// after run. Reset the exit state before launching to prevent restore.
	clearChromiumCrashState(paths[CHROMIUM])

	log.Debug().Msg("Launching browser process")
	u, err := l.Launch()
	if err != nil {
		// Context cancelled is expected on Ctrl+C
		if ctx.Err() != nil {
			return nil, func() {}
		}
		log.Fatal().Err(err).Msg("Failed to launch browser")
	}
	log.Debug().Str("controlURL", u).Msg("Browser process launched, connecting")

	browser := rod.New().Context(ctx).ControlURL(u)
	err = browser.Connect()
	if err != nil {
		if ctx.Err() != nil {
			return nil, func() {}
		}
		log.Fatal().Err(err).Msg("Failed to connect to browser")
	}
	log.Debug().Msg("Browser connected")

	cleanup := func() {
		log.Debug().Msg("Cleaning up browser")
		if browser != nil {
			if err := browser.Close(); err != nil {
				log.Error().Err(err).Msg("Failed to close browser")
			}
		}
		l.Kill()
		log.Debug().Msg("Browser launcher killed")
	}

	return browser, cleanup
}

// clearChromiumCrashState rewrites the profile's exit_type to "Normal" so the
// next launch does not restore tabs from the previous session. The CLI exits
// via os.Exit so the browser is never closed cleanly; Chromium then restores
// the prior tabs from Default/Sessions, piling up pages run after run. Deleting
// those session files removes the restore source, and resetting exit_type stops
// the "didn't shut down correctly" path. Both are best-effort.
func clearChromiumCrashState(userDataDir string) {
	defaultDir := filepath.Join(userDataDir, "Default")

	// Remove the saved session/tab files Chromium replays on restore.
	sessionsDir := filepath.Join(defaultDir, "Sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err == nil {
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, "Session_") || strings.HasPrefix(name, "Tabs_") {
				if err := os.Remove(filepath.Join(sessionsDir, name)); err != nil {
					log.Debug().Err(err).Str("file", name).Msg("Could not remove stale session file")
				}
			}
		}
		log.Debug().Msg("Cleared Chromium saved sessions to prevent tab restore")
	}

	// Reset exit_type so Chromium doesn't treat the prior os.Exit as a crash.
	prefsPath := filepath.Join(defaultDir, "Preferences")
	data, err := os.ReadFile(prefsPath)
	if err != nil {
		return
	}
	var prefs map[string]any
	if err := json.Unmarshal(data, &prefs); err != nil {
		log.Debug().Err(err).Msg("Could not parse Chromium Preferences, skipping exit_type reset")
		return
	}
	profile, ok := prefs["profile"].(map[string]any)
	if !ok || profile["exit_type"] == "Normal" {
		return
	}
	profile["exit_type"] = "Normal"
	profile["exited_cleanly"] = true
	updated, err := json.Marshal(prefs)
	if err != nil {
		log.Debug().Err(err).Msg("Could not re-encode Chromium Preferences, skipping exit_type reset")
		return
	}
	if err := os.WriteFile(prefsPath, updated, 0o600); err != nil {
		log.Debug().Err(err).Msg("Could not write Chromium Preferences, skipping exit_type reset")
	}
}

func setupRequestHijacker(router *rod.HijackRouter, samlResponseChan chan<- string) {
	err := router.Add("https://*amazon*", "", func(ctx *rod.Hijack) {
		reqURL := ctx.Request.URL().String()

		if reqURL == AWS_SAML_ENDPOINT || reqURL == AWS_GOV_SAML_ENDPOINT || reqURL == AWS_CN_SAML_ENDPOINT {
			log.Debug().Str("url", reqURL).Msg("Hijack: intercepted SAML endpoint, extracting response")
			val, err := url.ParseQuery(ctx.Request.Body())

			if err != nil {
				log.Fatal().Err(err).Msg("Failed to parse SAML endpoint response")
			}

			samlResponseChan <- val.Get("SAMLResponse")

			ctx.Response.Fail(proto.NetworkErrorReasonInternetDisconnected)
		} else {
			log.Debug().Str("url", reqURL).Msg("Hijack: continuing amazon request")
			ctx.ContinueRequest(&proto.FetchContinueRequest{})
		}
	})
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to add Amazon hijack route")
	}

	err = router.Add("https://*okta*", "", func(ctx *rod.Hijack) {
		reqURL, err := url.Parse(ctx.Request.URL().String())
		if err == nil {
			values := reqURL.Query()
			if values.Has("username") {
				values.Del("username")
				reqURL.RawQuery = values.Encode()

				ctx.ContinueRequest(&proto.FetchContinueRequest{URL: reqURL.String()})
				return
			}
		}
		ctx.ContinueRequest(&proto.FetchContinueRequest{})
	})
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to add Okta hijack route")
	}
}

func runStateLoop(ctx context.Context, page *rod.Page, samlResponseChan <-chan string, handlerCtx *HandlerContext, fastpass bool) string {
	samlResponse := ""

	// Bind context to page so all operations can be cancelled
	page = page.Context(ctx)

	// Debug mode: just wait for SAML response, don't auto-operate
	if handlerCtx.IsDebug {
		log.Debug().Msg("Debug mode: waiting for SAML response, no auto-fill")
		select {
		case r, ok := <-samlResponseChan:
			if ok {
				samlResponse = r
			}
		case <-ctx.Done():
			log.Debug().Msg("Context cancelled while waiting for SAML response")
			return ""
		}
	} else {
		log.Debug().Msg("Auto mode: entering state-detection loop")
		lastLoggedState := ""
	Loop:
		for {
			select {
			case <-ctx.Done():
				log.Debug().Msg("Context cancelled, exiting state loop")
				return ""
			case <-time.After(statePollInterval):
			}

			// Detect a dead browser (e.g. Chromium crashed): the CDP connection
			// breaks and element lookups silently fail forever, hanging the loop.
			if _, err := page.Info(); err != nil {
				log.Error().Err(err).Msg("Browser/page no longer reachable, aborting login (did Chromium crash?)")
				return ""
			}

			for _, st := range states {
				select {
				case r, ok := <-samlResponseChan:
					if ok {
						samlResponse = r
						break Loop
					}
				case <-ctx.Done():
					log.Debug().Msg("Context cancelled, exiting state loop")
					return ""
				default:
				}

				if (fastpass && (st.name == OKTA_SELECT_PUSH_FORM || st.name == OKTA_DO_PUSH_FORM)) || (!fastpass && st.name == OKTA_SELECT_FAST_PASS) {
					continue
				}

				el, err := page.Sleeper(rod.NotFoundSleeper).Element(st.selector)

				if err == nil {
					if st.name != lastLoggedState {
						log.Debug().Str("state", st.name).Str("selector", st.selector).Msg("State matched, running handler")
						lastLoggedState = st.name
					}
					st.handler(page, el, handlerCtx)
				}
			}
		}
	}

	log.Debug().Bool("gotSAML", samlResponse != "").Msg("State loop finished")
	return samlResponse
}

func performLogin(parentCtx context.Context, urlString string, noPrompt bool, defaultUserName string, defaultUserPassword *string, defaultOktaUserName *string, defaultOktaPassword *string, isGui bool, isDebug bool, showBrowser bool, disableLeakless bool, fastpass bool, useSystemBrowser bool) (string, error) {
	browser, cleanup := createBrowser(parentCtx, showBrowser, disableLeakless, useSystemBrowser)
	defer cleanup()

	if browser == nil {
		return "", nil
	}

	router := browser.HijackRequests()
	defer func() {
		if err := router.Stop(); err != nil {
			log.Error().Err(err).Msg("Failed to stop hijack router")
		}
	}()

	samlResponseChan := make(chan string, 1)
	setupRequestHijacker(router, samlResponseChan)

	go router.Run()

	log.Debug().Msg("Creating browser page")
	page, err := browser.Page(proto.TargetCreateTarget{URL: ""})
	if err != nil {
		return "", fmt.Errorf("create page: %w", err)
	}

	// Set viewport to match window size so content is properly centered
	err = page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:             WIDTH,
		Height:            HEIGHT,
		DeviceScaleFactor: 1,
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to set viewport")
	}

	// Create context that cancels on parent cancel or page close
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	// Listen for page/browser close events
	pageTargetID := page.TargetID
	go browser.EachEvent(func(e *proto.TargetTargetDestroyed) {
		if e.TargetID == pageTargetID {
			log.Debug().Str("targetID", string(e.TargetID)).Msg("Page target destroyed, cancelling login")
			cancel()
		}
	})()

	log.Debug().Str("url", urlString).Msg("Navigating to login URL")
	wait := page.WaitNavigation(proto.PageLifecycleEventNameDOMContentLoaded)
	err = page.Navigate(urlString)
	if err != nil {
		return "", fmt.Errorf("navigate to login URL %s: %w", urlString, err)
	}
	wait()
	log.Debug().Msg("Initial navigation complete (DOMContentLoaded)")

	handlerCtx := &HandlerContext{
		DefaultUserName:     defaultUserName,
		DefaultUserPassword: defaultUserPassword,
		DefaultOktaUserName: defaultOktaUserName,
		DefaultOktaPassword: defaultOktaPassword,
		NoPrompt:            noPrompt,
		IsGui:               isGui,
		IsDebug:             isDebug,
		PromptedStates:      make(map[string]bool),
	}

	return runStateLoop(ctx, page, samlResponseChan, handlerCtx, fastpass), nil
}

func performLoginWithBrowser(parentCtx context.Context, browser *rod.Browser, urlString string, noPrompt bool, defaultUserName string, defaultUserPassword *string, defaultOktaUserName *string, defaultOktaPassword *string, isGui bool, isDebug bool, fastpass bool) (string, error) {
	router := browser.HijackRequests()
	defer func() {
		if err := router.Stop(); err != nil {
			log.Error().Err(err).Msg("Failed to stop hijack router")
		}
	}()

	samlResponseChan := make(chan string, 1)
	setupRequestHijacker(router, samlResponseChan)

	go router.Run()

	page, err := browser.Page(proto.TargetCreateTarget{URL: ""})
	if err != nil {
		return "", fmt.Errorf("create page: %w", err)
	}

	// Set viewport to match window size so content is properly centered
	err = page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:             WIDTH,
		Height:            HEIGHT,
		DeviceScaleFactor: 1,
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to set viewport")
	}

	// Create context that cancels on parent cancel or page close
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	// Listen for page/browser close events
	pageTargetID := page.TargetID
	go browser.EachEvent(func(e *proto.TargetTargetDestroyed) {
		if e.TargetID == pageTargetID {
			cancel()
		}
	})()

	wait := page.WaitNavigation(proto.PageLifecycleEventNameDOMContentLoaded)
	err = page.Navigate(urlString)
	if err != nil {
		return "", fmt.Errorf("navigate to login URL %s: %w", urlString, err)
	}
	wait()

	handlerCtx := &HandlerContext{
		DefaultUserName:     defaultUserName,
		DefaultUserPassword: defaultUserPassword,
		DefaultOktaUserName: defaultOktaUserName,
		DefaultOktaPassword: defaultOktaPassword,
		NoPrompt:            noPrompt,
		IsGui:               isGui,
		IsDebug:             isDebug,
		PromptedStates:      make(map[string]bool),
	}

	samlResponse := runStateLoop(ctx, page, samlResponseChan, handlerCtx, fastpass)

	// Close the page after login to prepare for next profile
	if err := page.Close(); err != nil {
		log.Error().Err(err).Msg("Failed to close page")
	}

	return samlResponse, nil
}
