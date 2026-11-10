package detector

import (
	"testing"

	"github.com/angch/gogcbot/pkg/db"
)

func TestNewUserRussianTrigger_Evaluate(t *testing.T) {
	cfg := NewUserRussianTriggerConfig{
		Enabled:          true,
		MinHighUserID:    1000000000,
		MaxReputation:    5,
		MaxUserPosts:     5,
		MinCyrillicChars: 2,
		MinCyrillicRatio: 0.15,
		RepPenalty:       20,
	}
	trig := NewNewUserRussianTrigger(cfg)

	tests := []struct {
		name          string
		ctx           *TriggerContext
		wantTriggered bool
		wantActions   int
	}{
		{
			name: "Message 67376: Matching new high-ID user with 0 rep sending Russian crypto post",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     8760593389,
					Username:   "kristina_aveeel",
					FirstName:  "Evelina",
					Reputation: 0,
				},
				Text: "Буду покупать юсдт, пишите",
			},
			wantTriggered: true,
			wantActions:   3,
		},
		{
			name: "Message 67261: Matching new high-ID user sending Russian USDT buy request",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     1685842222,
					Reputation: 0,
				},
				Text: "Куплю USDT срочно",
			},
			wantTriggered: true,
			wantActions:   3,
		},
		{
			name: "Matching high-ID user on 2nd post with 1 rep sending Russian spam",
			ctx: &TriggerContext{
				IsNewUser:        false,
				UserMessageCount: 2,
				User: &db.User{
					UserID:     8760593389,
					Reputation: 1,
				},
				Text: "Буду покупать юсдт, пишите",
			},
			wantTriggered: true,
			wantActions:   3,
		},
		{
			name: "Exempt: User with reputation > 5 (rep = 10)",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     8760593389,
					Reputation: 10,
				},
				Text: "Буду покупать юсдт, пишите",
			},
			wantTriggered: false,
		},
		{
			name: "Exempt: Admin user",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     8760593389,
					Reputation: 0,
					IsAdmin:    true,
				},
				Text: "Буду покупать юсдт, пишите",
			},
			wantTriggered: false,
		},
		{
			name: "Exempt: Low ID user (e.g. established Telegram account < 1B)",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     12345678,
					Reputation: 0,
				},
				Text: "Буду покупать юсдт, пишите",
			},
			wantTriggered: false,
		},
		{
			name: "Exempt: Shieldy captcha verification text",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     8760593389,
					Reputation: 0,
				},
				Text: "I am not a bot",
			},
			wantTriggered: false,
		},
		{
			name: "Exempt: Regular English message",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     8760593389,
					Reputation: 0,
				},
				Text: "Hello everyone, glad to be here to learn Go!",
			},
			wantTriggered: false,
		},
		{
			name: "Exempt: Established user with > 5 messages",
			ctx: &TriggerContext{
				IsNewUser:        false,
				UserMessageCount: 10,
				User: &db.User{
					UserID:     8760593389,
					Reputation: 0,
				},
				Text: "Буду покупать юсдт, пишите",
			},
			wantTriggered: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := trig.Evaluate(tt.ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Triggered != tt.wantTriggered {
				t.Errorf("triggered = %v, want %v", res.Triggered, tt.wantTriggered)
			}
			if tt.wantTriggered {
				if len(res.Actions) != tt.wantActions {
					t.Errorf("actions count = %d, want %d", len(res.Actions), tt.wantActions)
				}
				hasDelete := false
				hasBan := false
				hasRep := false
				for _, a := range res.Actions {
					if a.Type == ActionDeleteMessage {
						hasDelete = true
					}
					if a.Type == ActionBanUser {
						hasBan = true
					}
					if a.Type == ActionAdjustReputation && a.RepDelta == -20 {
						hasRep = true
					}
				}
				if !hasDelete || !hasBan || !hasRep {
					t.Errorf("expected DeleteMessage, BanUser, and AdjustReputation(-20), got: %+v", res.Actions)
				}
			}
		})
	}
}

func TestNewUserRussianTrigger_ContainsCyrillic(t *testing.T) {
	cfg := NewUserRussianTriggerConfig{
		Enabled:          true,
		MinCyrillicChars: 2,
		MinCyrillicRatio: 0.15,
	}
	trig := NewNewUserRussianTrigger(cfg)

	tests := []struct {
		name          string
		text          string
		wantTriggered bool
	}{
		{
			name:          "Message 67376: Full Russian crypto spam",
			text:          "Буду покупать юсдт, пишите",
			wantTriggered: true,
		},
		{
			name:          "Message 67261: Russian + English USDT",
			text:          "Куплю USDT срочно",
			wantTriggered: true,
		},
		{
			name:          "Short Russian phrase",
			text:          "Привет всем",
			wantTriggered: true,
		},
		{
			name:          "English text only",
			text:          "Hello world! This is a test message.",
			wantTriggered: false,
		},
		{
			name:          "Chinese text (CJK script)",
			text:          "微信ZFB收钱 新人.一天8OO0",
			wantTriggered: false,
		},
		{
			name:          "Empty string",
			text:          "",
			wantTriggered: false,
		},
		{
			name:          "Whitespace only",
			text:          "    \t\n  ",
			wantTriggered: false,
		},
		{
			name: "Long English post with one incidental Cyrillic character (accidental typo or quote)",
			text: "In Go, goroutines are lightweight execution threads managed by the Go runtime. " +
				"They use segmented stacks that grow and shrink as needed, allowing thousands of concurrent goroutines. " +
				"Here is a small test word: б in an English paragraph.",
			wantTriggered: false,
		},
		{
			name: "Long Russian post (> 150 chars)",
			text: "Привет всем участникам этого чата! Мы предлагаем лучший сервис по обмену криптовалют. " +
				"Быстрый вывод средств, выгодный курс обмена и круглосуточная поддержка клиентов. " +
				"Пишите нам прямо сейчас для получения бонуса!",
			wantTriggered: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trig.ContainsCyrillic(tt.text)
			if got != tt.wantTriggered {
				t.Errorf("ContainsCyrillic(%q) = %v, want %v", tt.text, got, tt.wantTriggered)
			}
			if trig.ContainsRussian(tt.text) != got {
				t.Errorf("ContainsRussian mismatch with ContainsCyrillic")
			}
		})
	}
}

func TestNewUserRussianTrigger_Aliases(t *testing.T) {
	cfg := NewUserCyrillicTriggerConfig{
		Enabled: true,
	}
	trig := NewNewUserCyrillicTrigger(cfg)
	if trig.ID() != "new_user_russian" {
		t.Errorf("expected ID 'new_user_russian', got %s", trig.ID())
	}
	if !trig.IsEnabled() {
		t.Errorf("expected enabled")
	}
}
