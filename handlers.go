package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

var states = []state{
	{
		name:     "pick an account",
		selector: `div.table[role="button"][data-test-id]`,
		handler: func(pg *rod.Page, el *rod.Element, ctx *HandlerContext) {
			time.Sleep(time.Millisecond * 300)
			// Click the account button directly using rod
			if len(ctx.DefaultUserName) > 0 {
				// Try exact match first
				exactSelector := fmt.Sprintf(`div.table[role="button"][data-test-id="%s"]`, ctx.DefaultUserName)
				btn, err := pg.Timeout(2 * time.Second).Element(exactSelector)
				if err == nil && btn != nil {
					btn.Click(proto.InputMouseButtonLeft, 1)
					time.Sleep(time.Millisecond * 500)
					return
				}
			}
			// Fallback: click the found element
			if el != nil {
				el.Click(proto.InputMouseButtonLeft, 1)
				time.Sleep(time.Millisecond * 500)
			}
		},
	},
	{
		name:     "username input",
		selector: `input[name="loginfmt"]:not(.moveOffScreen)`,
		handler: func(pg *rod.Page, el *rod.Element, ctx *HandlerContext) {
			// Skip if already prompted for this state
			if ctx.PromptedStates["azure_username"] {
				return
			}

			// The field can match in the DOM before it has finished rendering
			// (visible=false). Don't mark the state handled until we've actually
			// filled it, so the next loop iteration retries once it's visible.
			visible, err := el.Visible()
			if err != nil || !visible {
				log.Debug().Bool("visible", visible).Err(err).Msg("username field not visible yet, will retry next iteration")
				return
			}

			username := ctx.DefaultUserName

			if !ctx.NoPrompt && !ctx.IsGui {
				prompt := &survey.Input{
					Message: "Azure Username:",
					Default: ctx.DefaultUserName,
				}
				survey.AskOne(prompt, &username, survey.WithValidator(survey.Required))
			}

			ctx.PromptedStates["azure_username"] = true

			if len(username) > 0 {
				log.Debug().Str("username", username).Msg("Filling Azure username")
				el.MustSelectAllText().MustInput("")
				el.MustInput(username)

				sb := pg.MustElement(`input[type=submit]`)

				sb.MustWaitVisible()
				wait := pg.MustWaitRequestIdle()
				sb.MustClick()
				wait()

				pContext := pg.GetContext()
				defer func() {
					pg.Context(pContext)
				}()

				goCtx, cancel := context.WithCancel(pContext)
				defer cancel()

				ch := make(chan bool, 1)

				go func() {
					for {
						select {
						case <-goCtx.Done():
							return
						default:
							_, err := pg.Sleeper(rod.NotFoundSleeper).Element("input[name=loginfmt]")
							if err != nil {
								ch <- true
								return
							}
						}
					}
				}()

				go func() {
					pg.Timeout(20 * time.Second).Race().
						Element("input[name=loginfmt].has-error").
						Element("input[name=loginfmt].moveOffScreen").
						Element("input[name=loginfmt]").Handle(func(e *rod.Element) error {
						return e.WaitInvisible()
					}).Do()

					select {
					case <-goCtx.Done():
						return
					default:
						ch <- true
						return
					}
				}()

				select {
				case <-ch:
				case <-time.After(25 * time.Second):
				}
			}
		},
	},
	{
		name:     "password input",
		selector: `input[name="Password"]:not(.moveOffScreen),input[name="passwd"]:not(.moveOffScreen)`,
		handler: func(pg *rod.Page, el *rod.Element, ctx *HandlerContext) {
			// Skip if already prompted for this state
			if ctx.PromptedStates["azure_password"] {
				return
			}

			alert, err := pg.Sleeper(rod.NotFoundSleeper).Element(".alert-error")

			if alert != nil && err == nil {
				fmt.Println(alert.Text())
			}

			var password string = ""

			if ctx.NoPrompt && ctx.DefaultUserPassword != nil {
				password = *ctx.DefaultUserPassword
			} else if !ctx.IsGui {
				prompt := &survey.Password{
					Message: "Azure Password",
				}
				survey.AskOne(prompt, &password, survey.WithValidator(survey.Required))
			}

			ctx.PromptedStates["azure_password"] = true

			if len(password) > 0 {
				el.MustWaitVisible()
				el.MustSelectAllText().MustInput("")
				el.MustInput(password)

				wait := pg.MustWaitRequestIdle()
				pg.MustElement("span[class=submit],input[type=submit]").MustClick()
				wait()

				time.Sleep(time.Millisecond * 500)
			}
		},
	},
	{
		name:     "OKTA username input",
		selector: `form:not(.o-form-saving) > div span.okta-form-input-field input[name="identifier"]:not([disabled])`,
		handler: func(pg *rod.Page, el *rod.Element, ctx *HandlerContext) {
			// Skip if already prompted for this state
			if ctx.PromptedStates["okta_username"] {
				return
			}

			errorSelector := `div.o-form-error-container`
			errorContainer, err := pg.Sleeper(rod.NotFoundSleeper).Element(errorSelector)

			if errorContainer != nil && err == nil {
				t, _ := errorContainer.Text()
				if t != "" {
					fmt.Println(t)
				}
			}

			infoSelector := `div.o-form-info-container`
			infoContainer, err := pg.Sleeper(rod.NotFoundSleeper).Element(infoSelector)
			if infoContainer != nil && err == nil {
				t, _ := infoContainer.Text()
				if t != "" {
					fmt.Println(t)
				}
			}

			var username string = ""

			if ctx.NoPrompt {
				if ctx.DefaultOktaUserName != nil {
					username = *ctx.DefaultOktaUserName
				} else {
					username = ctx.DefaultUserName
				}
			} else if !ctx.IsGui {
				defUser := ctx.DefaultUserName
				if ctx.DefaultOktaUserName != nil {
					defUser = *ctx.DefaultOktaUserName
				}
				promptUsername := &survey.Input{
					Message: "Okta Username:",
					Default: defUser,
				}
				survey.AskOne(promptUsername, &username, survey.WithValidator(survey.Required))
			}

			ctx.PromptedStates["okta_username"] = true

			if len(username) > 0 {

				el.MustWaitVisible()
				el.MustSelectAllText().MustInput("")
				el.MustInput(username)

				inputSelector := `form:not(.o-form-saving) > div span.okta-form-input-field input[name="identifier"]:not([disabled])`

				btn, err := pg.Sleeper(rod.NotFoundSleeper).Element(`input:not([disabled]):not(.link-button-disabled):not(.btn-disabled)[type=submit]`)
				if err == nil {
					wait := pg.MustWaitRequestIdle()
					btn.MustClick()
					wait()

					pContext := pg.GetContext()
					defer func() {
						pg.Context(pContext)
					}()

					goCtx, cancel := context.WithCancel(pContext)
					defer cancel()

					ch := make(chan bool, 1)

					go func() {
						for {
							select {
							case <-goCtx.Done():
								return
							default:
								_, err := pg.Sleeper(rod.NotFoundSleeper).Element(inputSelector)
								if err != nil {
									ch <- true
									return
								}
							}
						}
					}()

					go func() {
						pg.Timeout(20 * time.Second).Race().
							Element(errorSelector + `.o-form-has-errors`).Handle(func(e *rod.Element) error {
							if e != nil {
								t, _ := e.Text()
								if t != "" {
									return errors.New("error returned")
								}
							}
							return nil
						}).
							Element(inputSelector).Handle(func(e *rod.Element) error {
							return e.WaitInvisible()
						}).Do()

						select {
						case <-goCtx.Done():
							return
						default:
							ch <- true
							return
						}
					}()

					select {
					case <-ch:
					case <-time.After(25 * time.Second):
					}
				}
			}
		},
	},
	{
		name:     "OKTA password input",
		selector: `form:not(.o-form-saving) > div span.okta-form-input-field input[type="password"]:not([disabled])`,
		handler: func(pg *rod.Page, el *rod.Element, ctx *HandlerContext) {
			// Skip if already prompted for this state
			if ctx.PromptedStates["okta_password"] {
				return
			}

			errorSelector := `div.o-form-error-container`
			errorContainer, err := pg.Sleeper(rod.NotFoundSleeper).Element(errorSelector)

			if errorContainer != nil && err == nil {
				t, _ := errorContainer.Text()
				if t != "" {
					fmt.Println(t)
				}
			}

			infoSelector := `div.o-form-info-container`
			infoContainer, err := pg.Sleeper(rod.NotFoundSleeper).Element(infoSelector)
			if infoContainer != nil && err == nil {
				t, _ := infoContainer.Text()
				if t != "" {
					fmt.Println(t)
				}
			}

			var password string = ""
			shouldAskPassword := true

			if ctx.NoPrompt {
				if ctx.DefaultOktaPassword != nil {
					password = *ctx.DefaultOktaPassword
					shouldAskPassword = false
				} else if ctx.DefaultUserPassword != nil {
					password = *ctx.DefaultUserPassword
					shouldAskPassword = false
				}
			}

			if shouldAskPassword && !ctx.IsGui {
				promptPasswd := &survey.Password{
					Message: "Okta Password:",
				}
				survey.AskOne(promptPasswd, &password, survey.WithValidator(survey.Required))
			}

			ctx.PromptedStates["okta_password"] = true

			if len(password) > 0 {

				time.Sleep(time.Millisecond * 500)

				el.MustWaitVisible()
				el.MustSelectAllText().MustInput("")
				el.MustInput(password)

				inputSelector := `form:not(.o-form-saving) > div span.okta-form-input-field input[type="password"]:not([disabled])`

				btn, err := pg.Sleeper(rod.NotFoundSleeper).Element(`input:not([disabled]):not(.link-button-disabled):not(.btn-disabled)[type=submit]`)
				if err == nil {
					wait := pg.MustWaitRequestIdle()
					btn.MustClick()
					wait()

					pContext := pg.GetContext()
					defer func() {
						pg.Context(pContext)
					}()

					goCtx, cancel := context.WithCancel(pContext)
					defer cancel()

					ch := make(chan bool, 1)

					go func() {
						for {
							select {
							case <-goCtx.Done():
								return
							default:
								_, err := pg.Sleeper(rod.NotFoundSleeper).Element(inputSelector)
								if err != nil {
									ch <- true
									return
								}
							}
						}
					}()

					go func() {
						pg.Timeout(20 * time.Second).Race().
							Element(errorSelector + `.o-form-has-errors`).Handle(func(e *rod.Element) error {
							if e != nil {
								t, _ := e.Text()
								if t != "" {
									return errors.New("error returned")
								}
							}
							return nil
						}).
							Element(inputSelector).Handle(func(e *rod.Element) error {
							return e.WaitInvisible()
						}).Do()

						select {
						case <-goCtx.Done():
							return
						default:
							ch <- true
							return
						}
					}()

					select {
					case <-ch:
					case <-time.After(25 * time.Second):
					}
				}
			}
		},
	},
	{
		name:     OKTA_SELECT_FAST_PASS,
		selector: `div[data-se="okta_verify-signed_nonce"] > a:not([disabled]):not(.link-button-disabled):not(.btn-disabled)`,
		handler: func(pg *rod.Page, el *rod.Element, ctx *HandlerContext) {
			alert, err := pg.Sleeper(rod.NotFoundSleeper).Element(".infobox-error")

			if alert != nil && err == nil {
				t, _ := alert.Text()
				if t != "" {
					fmt.Println(t)
				}
			}

			btn, err := pg.Sleeper(rod.NotFoundSleeper).Element(`div[data-se="okta_verify-signed_nonce"] > a:not([disabled]):not(.btn-disabled):not(.link-button-disabled)`)
			if err == nil && btn != nil {
				btn.MustWaitVisible()
				wait := pg.MustWaitRequestIdle()
				btn.MustClick()
				wait()
				time.Sleep(time.Millisecond * 500)
			}
		},
	},
	{
		name:     OKTA_SELECT_PUSH_FORM,
		selector: `div[data-se="okta_verify-push"] > a:not([disabled]):not(.link-button-disabled):not(.btn-disabled)`,
		handler: func(pg *rod.Page, el *rod.Element, ctx *HandlerContext) {
			alert, err := pg.Sleeper(rod.NotFoundSleeper).Element(".infobox-error")

			if alert != nil && err == nil {
				t, _ := alert.Text()
				if t != "" {
					fmt.Println(t)
				}
			}

			btn, err := pg.Sleeper(rod.NotFoundSleeper).Element(`div[data-se="okta_verify-push"] > a:not([disabled]):not(.btn-disabled):not(.link-button-disabled)`)
			if err == nil && btn != nil {
				btn.MustWaitVisible()
				wait := pg.MustWaitRequestIdle()
				btn.MustClick()
				wait()
				time.Sleep(time.Millisecond * 500)
			}
		},
	},
	{
		name:     OKTA_DO_PUSH_FORM,
		selector: `a.send-push:not([disabled]):not(.link-button-disabled):not(.btn-disabled)`,
		handler: func(pg *rod.Page, el *rod.Element, ctx *HandlerContext) {
			alert, err := pg.Sleeper(rod.NotFoundSleeper).Element(".infobox-error")

			if alert != nil && err == nil {
				t, _ := alert.Text()
				if t != "" {
					fmt.Println(t)
				}
			}

			btn, err := pg.Sleeper(rod.NotFoundSleeper).Element(`a.send-push:not([disabled]):not(.btn-disabled):not(.link-button-disabled)`)
			if err == nil && btn != nil {
				btn.MustWaitVisible()
				wait := pg.MustWaitRequestIdle()
				btn.MustClick()
				wait()
				time.Sleep(time.Millisecond * 500)
			}
		},
	},
}
