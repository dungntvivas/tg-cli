// Package auth runs interactive first-run authentication using gotd's auth flow.
package auth

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
)

// Run prompts for phone + code + (optional 2FA password) and persists the session
// via gotd's storage (already configured in telegram.New). After Run returns nil,
// subsequent telegram.New() calls will load the persisted session.
//
// client is the raw gotd *telegram.Client; the session is stored in the
// FileStorage passed to telegram.NewClientOptions at construction time.
//
// The prompts are intentionally driven by gotd's flow: it calls Phone() → sends
// the code via SendCode() → calls Code() → (if 2FA required) calls Password().
// Pre-prompting for the code would race Telegram's send — the user would be
// asked for "the code Telegram sent" before Telegram actually sent it.
func Run(ctx context.Context, client *telegram.Client) error {
	reader := bufio.NewReader(os.Stdin)
	authenticator := &promptAuth{
		prompt: func(label string) (string, error) {
			fmt.Print(label)
			line, err := reader.ReadString('\n')
			if err != nil {
				return "", fmt.Errorf("read input: %w", err)
			}
			return strings.TrimSpace(line), nil
		},
	}
	flow := auth.NewFlow(authenticator, auth.SendCodeOptions{})

	return client.Run(ctx, func(ctx context.Context) error {
		if err := flow.Run(ctx, client.Auth()); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
		return nil
	})
}

// promptAuth implements auth.UserAuthenticator with lazy prompts driven by
// gotd's flow. No fields are pre-collected; each method reads on demand.
type promptAuth struct {
	prompt func(label string) (string, error)
}

func (a *promptAuth) Phone(_ context.Context) (string, error) {
	return a.prompt("Enter phone number (international format, e.g. +84...): ")
}

func (a *promptAuth) Code(_ context.Context, _ *tg.AuthSentCode) (string, error) {
	return a.prompt("Enter the code Telegram sent: ")
}

// Password is called by gotd's flow only when Telegram reports 2FA is required
// (ErrPasswordAuthNeeded). On empty input we return ErrPasswordNotProvided so
// the flow surfaces a meaningful error instead of attempting SignIn with "".
func (a *promptAuth) Password(_ context.Context) (string, error) {
	pw, err := a.prompt("Enter 2FA password: ")
	if err != nil {
		return "", err
	}
	if pw == "" {
		return "", auth.ErrPasswordNotProvided
	}
	return pw, nil
}

// AcceptTermsOfService / SignUp are only invoked if Telegram reports the account
// does not exist. First-run auth assumes the account exists, so we surface a
// SignUpRequired error instead of silently accepting.
func (a *promptAuth) AcceptTermsOfService(_ context.Context, tos tg.HelpTermsOfService) error {
	return &auth.SignUpRequired{TermsOfService: tos}
}

func (a *promptAuth) SignUp(_ context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, fmt.Errorf("sign up not supported")
}
