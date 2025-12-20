package main

import (
	"fmt"
	"net/url"
	"os"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

func createBrowser(showBrowser bool, disableLeakless bool, useSystemBrowser bool) (*rod.Browser, func()) {
	l := launcher.New().Headless(!showBrowser).UserDataDir(paths[CHROMIUM])

	if useSystemBrowser {
		if path, exists := launcher.LookPath(); exists {
			fmt.Printf("Using browser from %s\n", path)
			l.Bin(path)
		} else {
			fmt.Println("No usable system browser found, falling back to own Chromium browser")
		}
	}

	l.Leakless(!disableLeakless)
	l.Set("window-size", fmt.Sprintf("%d,%d", WIDTH, HEIGHT))

	u := l.MustLaunch()
	browser := rod.New().ControlURL(u).MustConnect()

	cleanup := func() {
		browser.MustClose()
	}

	return browser, cleanup
}

func setupRequestHijacker(router *rod.HijackRouter, samlResponseChan chan<- string) {
	router.MustAdd("https://*amazon*", func(ctx *rod.Hijack) {
		reqURL := ctx.Request.URL().String()

		if reqURL == AWS_SAML_ENDPOINT || reqURL == AWS_GOV_SAML_ENDPOINT || reqURL == AWS_CN_SAML_ENDPOINT {
			val, err := url.ParseQuery(ctx.Request.Body())

			if err != nil {
				fmt.Printf("Fail to saml endpoint response: %v", err)
				os.Exit(1)
			}

			samlResponseChan <- val.Get("SAMLResponse")

			ctx.Response.Fail(proto.NetworkErrorReasonInternetDisconnected)
		} else {
			ctx.ContinueRequest(&proto.FetchContinueRequest{})
		}
	})

	router.MustAdd("https://*okta*", func(ctx *rod.Hijack) {
		reqURL, error := url.Parse(ctx.Request.URL().String())
		if error == nil {
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
	defer router.MustStop()

	samlResponseChan := make(chan string, 1)
	setupRequestHijacker(router, samlResponseChan)

	go router.Run()

	page := browser.MustPage()
	wait := page.WaitNavigation(proto.PageLifecycleEventNameDOMContentLoaded)
	err := page.Navigate(urlString)
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
	}

	return runStateLoop(page, samlResponseChan, handlerCtx, fastpass)
}

func performLoginWithBrowser(browser *rod.Browser, urlString string, noPrompt bool, defaultUserName string, defaultUserPassword *string, defaultOktaUserName *string, defaultOktaPassword *string, isGui bool, fastpass bool) string {
	router := browser.HijackRequests()
	defer router.MustStop()

	samlResponseChan := make(chan string, 1)
	setupRequestHijacker(router, samlResponseChan)

	go router.Run()

	page := browser.MustPage()
	wait := page.WaitNavigation(proto.PageLifecycleEventNameDOMContentLoaded)
	err := page.Navigate(urlString)
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
	}

	samlResponse := runStateLoop(page, samlResponseChan, handlerCtx, fastpass)

	// Close the page after login to prepare for next profile
	page.MustClose()

	return samlResponse
}
