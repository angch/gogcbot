package detector

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/angch/gogcbot/pkg/db"
)

// ollamaMock returns an httptest server that responds to /api/generate with the
// given verdict, and records the last request body for assertions.
func ollamaMock(verdict string) (*httptest.Server, *string) {
	body := new(string)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			http.NotFound(w, r)
			return
		}
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		*body = string(buf)
		_, _ = w.Write([]byte(`{"response":"` + verdict + `"}`))
	}))
	return srv, body
}

func TestOllamaNameClassifierTrigger_Banned(t *testing.T) {
	srv, body := ollamaMock("BANNED")
	defer srv.Close()

	cfg := OllamaNameClassifierConfig{
		Enabled:        true,
		OllamaURL:      srv.URL,
		Model:          "phi4",
		MinHighUserID:  1000000000,
		MaxReputation:  5,
		MaxUserPosts:   5,
		RequestTimeout: 5 * time.Second,
		FlagOnly:       true,
	}
	trig := newOllamaNameClassifierTrigger(cfg, srv.Client(), 5*time.Second)

	ctx := &TriggerContext{
		IsNewUser: true,
		User:      &db.User{UserID: 5000000000, Reputation: 0, Username: "lwbGMfQOslOWDtGUycbPEhZn", FirstName: "六o0", LastName: "壹天"},
	}
	res, err := trig.Evaluate(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Triggered {
		t.Fatalf("expected triggered for BANNED verdict, got false")
	}
	if len(res.Actions) != 1 || res.Actions[0].Type != ActionFlagMessage {
		t.Fatalf("flag_only should emit one flag action, got %#v", res.Actions)
	}
	// Model and both username+name must be sent in the request.
	if !strings.Contains(*body, "phi4") {
		t.Errorf("request body missing model: %s", *body)
	}
	if !strings.Contains(*body, "lwbGMfQOslOWDtGUycbPEhZn") && !strings.Contains(*body, "六o0") {
		t.Errorf("request body missing identification: %s", *body)
	}
}

func TestOllamaClassifierTrigger_Clean(t *testing.T) {
	srv, _ := ollamaMock("CLEAN")
	defer srv.Close()

	cfg := OllamaNameClassifierConfig{
		Enabled:        true,
		OllamaURL:      srv.URL,
		Model:          "phi4",
		MinHighUserID:  1000000000,
		MaxUserPosts:   1,
		RequestTimeout: 5 * time.Second,
	}
	trig := newOllamaNameClassifierTrigger(cfg, srv.Client(), 5*time.Second)

	ctx := &TriggerContext{
		IsNewUser: true,
		User:      &db.User{UserID: 5000000000, Reputation: 0, Username: "angch", FirstName: "Ang", LastName: "ChinHan"},
	}
	res, err := trig.Evaluate(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Triggered {
		t.Fatalf("expected not triggered for CLEAN, got true (reason: %s)", res.Reason)
	}
}

func TestOllamaClassifierTrigger_EvaluateSkips(t *testing.T) {
	srv, _ := ollamaMock("BANNED")
	defer srv.Close()

	cfg := OllamaNameClassifierConfig{
		Enabled:        true,
		OllamaURL:      srv.URL,
		Model:          "phi4",
		MinHighUserID:  1000000000,
		MaxReputation:  5,
		MaxUserPosts:   5,
		RequestTimeout: 5 * time.Second,
	}
	trig := newOllamaNameClassifierTrigger(cfg, srv.Client(), 5*time.Second)

	cases := []struct {
		name string
		ctx  *TriggerContext
	}{
		{
			name: "established user skipped",
			ctx:  &TriggerContext{IsNewUser: false, UserMessageCount: 15, User: &db.User{UserID: 5000000000, FirstName: "六o0壹天"}},
		},
		{
			name: "low ID skipped",
			ctx:  &TriggerContext{IsNewUser: true, User: &db.User{UserID: 123, FirstName: "六o0壹天"}},
		},
		{
			name: "high rep skipped",
			ctx:  &TriggerContext{IsNewUser: true, User: &db.User{UserID: 5000000000, Reputation: 50, FirstName: "六o0壹天"}},
		},
		{
			name: "no name or username skipped",
			ctx:  &TriggerContext{IsNewUser: true, User: &db.User{UserID: 5000000000}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := trig.Evaluate(c.ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Triggered {
				t.Fatalf("expected not triggered, got true")
			}
		})
	}
}

func TestOllamaClassifierTrigger_HTTPError(t *testing.T) {
	// Server that always errors - classifier must propagate the failure, not
	// silently classify clean.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := OllamaNameClassifierConfig{
		Enabled:        true,
		OllamaURL:      srv.URL,
		Model:          "phi4",
		MinHighUserID:  1000000000,
		MaxUserPosts:   5,
		RequestTimeout: 2 * time.Second,
	}
	trig := newOllamaNameClassifierTrigger(cfg, srv.Client(), 5*time.Second)
	res, err := trig.Evaluate(&TriggerContext{
		IsNewUser: true,
		User:      &db.User{UserID: 5000000000, FirstName: "六o0壹天"},
	})
	if err == nil {
		t.Fatalf("expected error on HTTP failure, got nil (triggered=%v)", res != nil && res.Triggered)
	}
}
