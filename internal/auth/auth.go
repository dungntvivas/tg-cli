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
func Run(ctx context.Context, client *telegram.Client) error {
	reader := bufio.NewReader(os.Stdin)

	phone, err := prompt(reader, "Enter phone number (international format, e.g. +84...): ")
	if err != nil {
		return err
	}

	code, err := prompt(reader, "Enter the code Telegram sent: ")
	if err != nil {
		return err
	}

	// Custom authenticator: 2FA password is prompted only if Telegram requests it
	// (gotd's Flow calls Auth.Password(ctx) on ErrPasswordAuthNeeded, so we read
	// it lazily here).
	authenticator := &promptAuth{
		reader: reader,
		phone:  phone,
		code:   code,
	}

	flow := auth.NewFlow(authenticator, auth.SendCodeOptions{})

	return client.Run(ctx, func(ctx context.Context) error {
		// Flow.Run returns plain error (not a tuple); on success, the session
		// is already persisted by FileStorage.
		if err := flow.Run(ctx, client.Auth()); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
		return nil
	})
}

// promptAuth holds the phone + code and prompts for 2FA password lazily, only
// when gotd's flow requests it.
type promptAuth struct {
	reader *bufio.Reader
	phone  string
	code   string
}

func (a *promptAuth) Phone(_ context.Context) (string, error) {
	return a.phone, nil
}

func (a *promptAuth) Code(_ context.Context, _ *tg.AuthSentCode) (string, error) {
	return a.code, nil
}

func (a *promptAuth) Password(_ context.Context) (string, error) {
	pw, err := prompt(a.reader, "Enter 2FA password: ")
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

func prompt(r *bufio.Reader, label string) (string, error) {
	fmt.Print(label)
	line, err := r.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimSpace(line), nil
}
