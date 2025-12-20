package main

import (
	"fmt"
	"net/url"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

func createBrowser(showBrowser bool, disableLeakless bool, useSystemBrowser bool) (*rod.Browser, func()) {
	l := launcher.New().Headless(!showBrowser).UserDataDir(paths[CHROMIUM])

	if useSystemBrowser {
		if path, exists := launcher.LookPath(); exists {
			log.Info().Str("path", path).Msg("Using system browser")
			l.Bin(path)
		} else {
			log.Warn().Msg("No usable system browser found, falling back to own Chromium browser")
		}
	}

	l.Leakless(!disableLeakless)
	l.Set("window-size", fmt.Sprintf("%d,%d", WIDTH, HEIGHT))

	u, err := l.Launch()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to launch browser")
	}

	browser := rod.New().ControlURL(u)
	err = browser.Connect()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to browser")
	}

	cleanup := func() {
		if err := browser.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to close browser")
		}
	}

	return browser, cleanup
}

func setupRequestHijacker(router *rod.HijackRouter, samlResponseChan chan<- string) {
	err := router.Add("https://*amazon*", "", func(ctx *rod.Hijack) {
		reqURL := ctx.Request.URL().String()

		if reqURL == AWS_SAML_ENDPOINT || reqURL == AWS_GOV_SAML_ENDPOINT || reqURL == AWS_CN_SAML_ENDPOINT {
			val, err := url.ParseQuery(ctx.Request.Body())

			if err != nil {
				log.Fatal().Err(err).Msg("Failed to parse SAML endpoint response")
			}

			samlResponseChan <- val.Get("SAMLResponse")

			ctx.Response.Fail(proto.NetworkErrorReasonInternetDisconnected)
		} else {
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

func runStateLoop(page *rod.Page, samlResponseChan <-chan string, handlerCtx *HandlerContext, fastpass bool) string {
	samlResponse := ""

	if handlerCtx.IsGui && !handlerCtx.NoPrompt {
		r, ok := <-samlResponseChan
		if ok {
			samlResponse = r
		}
	} else {
	Loop:
		for {
			for _, st := range states {
				select {
				case r, ok := <-samlResponseChan:
					if ok {
						samlResponse = r
						break Loop
					}
				default:
				}

				if (fastpass && (st.name == OKTA_SELECT_PUSH_FORM || st.name == OKTA_DO_PUSH_FORM)) || (!fastpass && st.name == OKTA_SELECT_FAST_PASS) {
					continue
				}

				el, err := page.Sleeper(rod.NotFoundSleeper).Element(st.selector)

				if err == nil {
					st.handler(page, el, handlerCtx)
				}
			}
		}
	}

	return samlResponse
}

func performLogin(urlString string, noPrompt bool, defaultUserName string, defaultUserPassword *string, defaultOktaUserName *string, defaultOktaPassword *string, isGui bool, showBrowser bool, disableLeakless bool, fastpass bool, useSystemBrowser bool) string {
	browser, cleanup := createBrowser(showBrowser, disableLeakless, useSystemBrowser)
	defer cleanup()

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
		log.Fatal().Err(err).Msg("Failed to create page")
	}
	wait := page.WaitNavigation(proto.PageLifecycleEventNameDOMContentLoaded)
	err = page.Navigate(urlString)
	if err != nil {
		log.Fatal().Err(err).Str("url", urlString).Msg("Failed to navigate to login URL")
	}
	wait()

	handlerCtx := &HandlerContext{
		DefaultUserName:     defaultUserName,
		DefaultUserPassword: defaultUserPassword,
		DefaultOktaUserName: defaultOktaUserName,
		DefaultOktaPassword: defaultOktaPassword,
		NoPrompt:            noPrompt,
		IsGui:               isGui,
		PromptedStates:      make(map[string]bool),
	}

	return runStateLoop(page, samlResponseChan, handlerCtx, fastpass)
}

func performLoginWithBrowser(browser *rod.Browser, urlString string, noPrompt bool, defaultUserName string, defaultUserPassword *string, defaultOktaUserName *string, defaultOktaPassword *string, isGui bool, fastpass bool) string {
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
		log.Fatal().Err(err).Msg("Failed to create page")
	}
	wait := page.WaitNavigation(proto.PageLifecycleEventNameDOMContentLoaded)
	err = page.Navigate(urlString)
	if err != nil {
		log.Fatal().Err(err).Str("url", urlString).Msg("Failed to navigate to login URL")
	}
	wait()

	handlerCtx := &HandlerContext{
		DefaultUserName:     defaultUserName,
		DefaultUserPassword: defaultUserPassword,
		DefaultOktaUserName: defaultOktaUserName,
		DefaultOktaPassword: defaultOktaPassword,
		NoPrompt:            noPrompt,
		IsGui:               isGui,
		PromptedStates:      make(map[string]bool),
	}

	samlResponse := runStateLoop(page, samlResponseChan, handlerCtx, fastpass)

	// Close the page after login to prepare for next profile
	if err := page.Close(); err != nil {
		log.Error().Err(err).Msg("Failed to close page")
	}

	return samlResponse
}
