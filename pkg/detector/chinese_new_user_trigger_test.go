package detector

import (
	"testing"

	"github.com/angch/gogcbot/pkg/db"
	lingua "github.com/pemistahl/lingua-go"
)

func TestNewUserChineseTrigger_Evaluate(t *testing.T) {
	ld := lingua.NewLanguageDetectorBuilder().FromAllLanguages().Build()
	cfg := NewUserChineseTriggerConfig{
		Enabled:       true,
		MinHighUserID: 1000000000,
		MaxReputation: 0,
		RepPenalty:    20,
	}
	trig := NewNewUserChineseTriggerWithDetector(cfg, ld)

	tests := []struct {
		name          string
		ctx           *TriggerContext
		wantTriggered bool
		wantActions   int
	}{
		{
			name: "Matching new high-ID user with 0 rep sending Chinese only",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     5000000000,
					Reputation: 0,
				},
				Text: "恭喜发财 万事如意",
			},
			wantTriggered: true,
			wantActions:   3,
		},
		{
			name: "Not new user (seen before)",
			ctx: &TriggerContext{
				IsNewUser: false,
				User: &db.User{
					UserID:     5000000000,
					Reputation: 0,
				},
				Text: "恭喜发财 万事如意",
			},
			wantTriggered: false,
			wantActions:   0,
		},
		{
			name: "Low ID user",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     123456,
					Reputation: 0,
				},
				Text: "恭喜发财 万事如意",
			},
			wantTriggered: false,
			wantActions:   0,
		},
		{
			name: "High rep user",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     5000000000,
					Reputation: 10,
				},
				Text: "恭喜发财 万事如意",
			},
			wantTriggered: false,
			wantActions:   0,
		},
		{
			name: "Matching new high-ID user sending 無風險有想法的莱",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     6890123456,
					Reputation: 0,
				},
				Text: "無風險有想法的莱",
			},
			wantTriggered: true,
			wantActions:   3,
		},
		{
			name: "English text message",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     5000000000,
					Reputation: 0,
				},
				Text: "Hello world this is English",
			},
			wantTriggered: false,
			wantActions:   0,
		},
		{
			name: "Mixed Chinese and English message",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     5000000000,
					Reputation: 0,
				},
				Text: "Hello 你好",
			},
			wantTriggered: true,
			wantActions:   3,
		},
		{
			name: "Edge case with filler English and bot mention plus Chinese",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     8841787542,
					Reputation: 0,
				},
				Text: "i test edge cases only. @hulksmashbannerbot 你是笨蛋吗？",
			},
			wantTriggered: true,
			wantActions:   3,
		},
		{
			name: "Shieldy verification text (I am not a bot) should not trigger Chinese ban",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     5000000000,
					Reputation: 0,
				},
				Text: "I am not a bot",
			},
			wantTriggered: false,
			wantActions:   0,
		},
		{
			name: "User with HasVerifiedNotBot flag set should not trigger Chinese ban even if message has Chinese",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     5000000000,
					Reputation: 0,
				},
				Text:              "恭喜发财 万事如意",
				HasVerifiedNotBot: true,
			},
			wantTriggered: false,
			wantActions:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := trig.Evaluate(tt.ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Triggered != tt.wantTriggered {
				t.Errorf("Triggered = %v, want %v", res.Triggered, tt.wantTriggered)
			}
			if len(res.Actions) != tt.wantActions {
				t.Errorf("Actions count = %d, want %d", len(res.Actions), tt.wantActions)
			}
			if res.Triggered {
				// Verify action details: Delete, Ban, Rep -20
				hasDelete := false
				hasBan := false
				hasRep := false
				for _, act := range res.Actions {
					if act.Type == ActionDeleteMessage {
						hasDelete = true
					}
					if act.Type == ActionBanUser {
						hasBan = true
					}
					if act.Type == ActionAdjustReputation && act.RepDelta == -20 {
						hasRep = true
					}
				}
				if !hasDelete || !hasBan || !hasRep {
					t.Errorf("Actions set incomplete or wrong: %#v", res.Actions)
				}
			}
		})
	}
}

func TestNewUserChineseTrigger_IsOnlyChinese(t *testing.T) {
	ld := lingua.NewLanguageDetectorBuilder().FromAllLanguages().Build()
	cfg := NewUserChineseTriggerConfig{Enabled: true}
	trig := NewNewUserChineseTriggerWithDetector(cfg, ld)

	tests := []struct {
		text string
		want bool
	}{
		{"你好世界", true},
		{"你好！今天怎么样？", true},
		{"恭喜发财 123", true},
		{"無風險有想法的莱", true},
		{"演员来", true},
		{"Hello 你好", true},
		{"i test edge cases only. @hulksmashbannerbot 你是笨蛋吗？", true},
		{"English text only", false},
		{"123456", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got := trig.IsOnlyChinese(tt.text)
			if got != tt.want {
				t.Errorf("IsOnlyChinese(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestIsShieldyVerificationText(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"I am not a bot", true},
		{"i am not a bot", true},
		{"I am not a bot.", true},
		{"  I   am   not   a   bot!  ", true},
		{"I'm not a bot", true},
		{"im not a bot", true},
		{"I am not a bot?", true},
		{"Hello world", false},
		{"Crypto giveaway I am not a bot", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got := IsShieldyVerificationText(tt.text)
			if got != tt.want {
				t.Errorf("IsShieldyVerificationText(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}
