package telegram

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFakeAPI_Dialogs(t *testing.T) {
	want := []Dialog{{ID: 1, Title: "Alice"}, {ID: 2, Title: "Bob"}}
	api := &FakeAPI{
		DialogsFn: func(ctx context.Context) ([]Dialog, error) {
			return want, nil
		},
	}

	got, err := api.Dialogs(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0].Title != "Alice" {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestFakeAPI_History(t *testing.T) {
	p := Peer{ID: 42, Kind: "user"}
	api := &FakeAPI{
		HistoryFn: func(ctx context.Context, peer Peer, limit int) ([]Message, error) {
			if peer != p {
				t.Errorf("peer = %+v, want %+v", peer, p)
			}
			if limit != 50 {
				t.Errorf("limit = %d, want 50", limit)
			}
			return []Message{{ID: 1, Text: "hi", Time: time.Now()}}, nil
		},
	}

	got, err := api.History(context.Background(), p, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Text != "hi" {
		t.Errorf("got %+v", got)
	}
}

func TestFakeAPI_Send(t *testing.T) {
	p := Peer{ID: 1, Kind: "user"}
	api := &FakeAPI{
		SendFn: func(ctx context.Context, peer Peer, text string) (Message, error) {
			if text != "hello" {
				t.Errorf("text = %q, want hello", text)
			}
			return Message{ID: 99, Text: text, Outgoing: true}, nil
		},
	}

	got, err := api.Send(context.Background(), p, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Outgoing || got.Text != "hello" {
		t.Errorf("got %+v", got)
	}
}

func TestFakeAPI_ErrorPropagates(t *testing.T) {
	wantErr := errors.New("network down")
	api := &FakeAPI{
		DialogsFn: func(ctx context.Context) ([]Dialog, error) { return nil, wantErr },
	}
	_, err := api.Dialogs(context.Background())
	if err != wantErr {
		t.Errorf("got %v, want %v", err, wantErr)
	}
}
