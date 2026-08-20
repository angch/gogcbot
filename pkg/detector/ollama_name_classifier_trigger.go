package detector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OllamaNameClassifierConfig configures an LLM-based profile-name classifier
// backed by a local Ollama server.
//
// Unlike the deterministic ProfileNameKeywordBanTrigger, this calls a hosted
// LLM (e.g. "phi4") at classification time. It therefore has a real latency and
// availability cost, and is evaluated synchronously on the message path, so it
// MUST be used with a short request timeout and flag_only: true in production.
type OllamaNameClassifierConfig struct {
	Enabled       bool   `mapstructure:"enabled" yaml:"enabled"`
	OllamaURL     string `mapstructure:"ollama_url" yaml:"ollama_url"`
	Model         string `mapstructure:"model" yaml:"model"`
	MinHighUserID int64  `mapstructure:"min_high_user_id" yaml:"min_high_user_id"`
	MaxReputation int    `mapstructure:"max_reputation" yaml:"max_reputation"`
	MaxUserPosts  int    `mapstructure:"max_user_posts" yaml:"max_user_posts"`
	// RequestTimeout is the per-call HTTP timeout for the classification round
	// trip. Keeps a slow/stalled model from blocking the message handler.
	RequestTimeout time.Duration `mapstructure:"request_timeout" yaml:"request_timeout"`
	// FlagOnly emits only a flag/report action instead of auto-banning.
	// Strongly recommended: LLM output is nondeterministic.
	FlagOnly   bool `mapstructure:"flag_only" yaml:"flag_only"`
	RepPenalty int  `mapstructure:"rep_penalty" yaml:"rep_penalty"`
}

// OllamaNameClassifierTrigger detects brand new, high-ID, low-rep users whose
// profile name + username an LLM judges to be spam-farm style.
type OllamaNameClassifierTrigger struct {
	cfg            OllamaNameClassifierConfig
	client         *http.Client
	requestTimeout time.Duration
}

// NewOllamaNameClassifierTrigger constructs a new trigger. When httpClient is
// nil a default client with the configured timeout is used.
func NewOllamaNameClassifierTrigger(cfg OllamaNameClassifierConfig) *OllamaNameClassifierTrigger {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return newOllamaNameClassifierTrigger(cfg, &http.Client{Timeout: timeout}, timeout)
}

func newOllamaNameClassifierTrigger(cfg OllamaNameClassifierConfig, client *http.Client, timeout time.Duration) *OllamaNameClassifierTrigger {
	return &OllamaNameClassifierTrigger{cfg: cfg, client: client, requestTimeout: timeout}
}

func (t *OllamaNameClassifierTrigger) ID() string {
	return "ollama_name_classifier"
}

func (t *OllamaNameClassifierTrigger) Name() string {
	return "LLM (Ollama) Spam Profile Name Classifier"
}

func (t *OllamaNameClassifierTrigger) IsEnabled() bool {
	return t.cfg.Enabled
}

func (t *OllamaNameClassifierTrigger) Evaluate(ctx *TriggerContext) (*TriggerResult, error) {
	if !t.IsEnabled() || ctx == nil {
		return &TriggerResult{Triggered: false}, nil
	}

	// Cohort gate (same as sibling triggers): only classify new high-ID low-rep
	// users, keeping LLM spend and latency bounded.
	maxPosts := t.cfg.MaxUserPosts
	if maxPosts <= 0 {
		maxPosts = 5
	}
	isNewUser := ctx.IsNewUser || (ctx.UserMessageCount > 0 && ctx.UserMessageCount <= maxPosts)
	if !isNewUser {
		return &TriggerResult{Triggered: false}, nil
	}
	if ctx.User == nil || ctx.User.UserID < t.cfg.MinHighUserID {
		return &TriggerResult{Triggered: false}, nil
	}
	if ctx.User.Reputation > t.cfg.MaxReputation {
		return &TriggerResult{Triggered: false}, nil
	}

	username := strings.TrimSpace(ctx.User.Username)
	name := strings.TrimSpace(ctx.User.FirstName + " " + ctx.User.LastName)
	if name == "" && username == "" {
		return &TriggerResult{Triggered: false}, nil
	}

	verdict, err := t.classify(ctx, username, name)
	if err != nil {
		return nil, err
	}
	if verdict != "BANNED" {
		return &TriggerResult{Triggered: false}, nil
	}

	repPenalty := t.cfg.RepPenalty
	if repPenalty <= 0 {
		repPenalty = 20
	}

	reason := "Detection trigger (ollama_name_classifier): High-ID new user with low/no rep classified as a spam/farm profile name by LLM"

	actions := []Action{
		{Type: ActionFlagMessage, Reason: reason},
	}
	if !t.cfg.FlagOnly {
		actions = append(actions,
			Action{Type: ActionDeleteMessage, Reason: reason},
			Action{Type: ActionBanUser, Reason: reason},
			Action{Type: ActionAdjustReputation, RepDelta: -repPenalty, Reason: reason},
		)
	}

	return &TriggerResult{
		Triggered: true,
		TriggerID: t.ID(),
		Reason:    reason,
		Actions:   actions,
	}, nil
}

const ollamaClassifierSystem = "You are a Telegram group moderator. Classify whether a user is a spam farm account based on their username and profile name. Respond with ONLY the single word BANNED or CLEAN. BANNED signals: auto-generated/farmed usernames with embedded digits mixing letter case (e.g. lwbGMfQOslOWDtGUycbPEhZn), profile names that are farm ad terms with homoglyph digits (e.g. 六o0壹天, 每日2000吴压), random consonant-junk names. CLEAN signals: normal human names and human-chosen usernames regardless of ethnicity."

func (t *OllamaNameClassifierTrigger) classify(ctx *TriggerContext, username, name string) (string, error) {
	baseURL := strings.TrimRight(t.cfg.OllamaURL, "/")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	model := t.cfg.Model
	if model == "" {
		model = "phi4"
	}

	prompt := fmt.Sprintf("Username: %s\nProfile name: %s\nClassification:", username, name)
	body, _ := json.Marshal(map[string]any{
		"model":   model,
		"system":  ollamaClassifierSystem,
		"prompt":  prompt,
		"stream":  false,
		"options": map[string]any{"temperature": 0},
	})

	reqCtx, cancel := context.WithTimeout(context.Background(), t.requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama generate: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("ollama status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var out struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode ollama response: %w", err)
	}
	upper := strings.ToUpper(strings.TrimSpace(out.Response))
	if strings.Contains(upper, "BANNED") {
		return "BANNED", nil
	}
	return "CLEAN", nil
}
