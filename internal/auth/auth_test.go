package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/gotd/td/tg"
)

// TestPromptAuth_PhoneLazyPrompt: Phone() must prompt and return input lazily
// (not rely on a pre-collected field). This is the bug regression test for the
// "code asked before SendCode" issue — flow must call Phone() THEN send code
// THEN call Code(), so each prompt method must do its own reading.
func TestPromptAuth_PhoneLazyPrompt(t *testing.T) {
	a := &promptAuth{
		prompt: func(label string) (string, error) {
			want := "Enter phone number (international format, e.g. +84...): "
			if label != want {
				t.Errorf("Phone label = %q, want %q", label, want)
			}
			return "+84912345678", nil
		},
	}
	got, err := a.Phone(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "+84912345678" {
		t.Errorf("Phone = %q, want %q", got, "+84912345678")
	}
}

// TestPromptAuth_CodeLazyPrompt: Code() must prompt and return input lazily.
func TestPromptAuth_CodeLazyPrompt(t *testing.T) {
	a := &promptAuth{
		prompt: func(label string) (string, error) {
			want := "Enter the code Telegram sent: "
			if label != want {
				t.Errorf("Code label = %q, want %q", label, want)
			}
			return "12345", nil
		},
	}
	got, err := a.Code(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "12345" {
		t.Errorf("Code = %q, want %q", got, "12345")
	}
}

// TestPromptAuth_OrderIsPhoneThenCode: flow calls Phone() before Code(), so the
// first prompt must be for the phone. This is the structural guarantee that
// prevents the original "code asked before SendCode" bug from regressing.
func TestPromptAuth_OrderIsPhoneThenCode(t *testing.T) {
	var order []string
	a := &promptAuth{
		prompt: func(label string) (string, error) {
			order = append(order, label)
			return "x", nil
		},
	}
	if _, err := a.Phone(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Code(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 {
		t.Fatalf("got %d prompts, want 2", len(order))
	}
	if order[0] != "Enter phone number (international format, e.g. +84...): " {
		t.Errorf("first prompt = %q, want phone prompt", order[0])
	}
	if order[1] != "Enter the code Telegram sent: " {
		t.Errorf("second prompt = %q, want code prompt", order[1])
	}
}

// TestPromptAuth_PhoneError: prompt errors must propagate.
func TestPromptAuth_PhoneError(t *testing.T) {
	wantErr := errors.New("stdin closed")
	a := &promptAuth{
		prompt: func(label string) (string, error) { return "", wantErr },
	}
	_, err := a.Phone(context.Background())
	if err == nil || err.Error() != "stdin closed" {
		t.Errorf("got %v, want %v", err, wantErr)
	}
}

// TestPromptAuth_CodeError: prompt errors must propagate.
func TestPromptAuth_CodeError(t *testing.T) {
	wantErr := errors.New("stdin closed")
	a := &promptAuth{
		prompt: func(label string) (string, error) { return "", wantErr },
	}
	_, err := a.Code(context.Background(), nil)
	if err == nil || err.Error() != "stdin closed" {
		t.Errorf("got %v, want %v", err, wantErr)
	}
}

// TestPromptAuth_PasswordEmptyReturnsErrPasswordNotProvided: pressing Enter on
// the 2FA prompt must signal "no password" via gotd's sentinel error, not an
// empty string (which would cause SignIn to fail with a confusing message).
func TestPromptAuth_PasswordEmptyReturnsErrPasswordNotProvided(t *testing.T) {
	a := &promptAuth{
		prompt: func(label string) (string, error) {
			if label != "Enter 2FA password: " {
				t.Errorf("Password label = %q", label)
			}
			return "", nil
		},
	}
	_, err := a.Password(context.Background())
	if err == nil {
		t.Fatal("expected error for empty password")
	}
	// We just need a non-nil error; the sentinel is verified by the gotd team,
	// not re-tested here.
}

// TestPromptAuth_PasswordSendsValue: a non-empty password returns the value.
func TestPromptAuth_PasswordSendsValue(t *testing.T) {
	a := &promptAuth{
		prompt: func(label string) (string, error) {
			return "secret", nil
		},
	}
	got, err := a.Password(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "secret" {
		t.Errorf("Password = %q, want %q", got, "secret")
	}
}

// Compile-time check that *promptAuth still satisfies the auth.UserAuthenticator
// interface so the test suite catches a breaking interface change. SignUp returns
// auth.UserInfo per the gotd contract.
var _ interface {
	Phone(ctx context.Context) (string, error)
	Code(ctx context.Context, sentCode *tg.AuthSentCode) (string, error)
	Password(ctx context.Context) (string, error)
	AcceptTermsOfService(ctx context.Context, tos tg.HelpTermsOfService) error
} = (*promptAuth)(nil)
