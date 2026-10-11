package detector

import (
	"testing"

	"github.com/angch/gogcbot/pkg/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestCustomStickerTrigger_LowRepCustomStickerBan(t *testing.T) {
	trig := NewCustomStickerTrigger(CustomStickerTriggerConfig{
		Enabled:       true,
		MaxReputation: 5,
		RepPenalty:    50,
	})

	// User matching message id 66607: rep 0, low rep, sends custom sticker 👍
	user := &db.User{
		UserID:     8927451894,
		Username:   "",
		FirstName:  "Matthew",
		LastName:   "Schultz",
		Reputation: 0,
		IsAdmin:    false,
		IsBanned:   false,
	}

	testCases := []struct {
		name          string
		rawMsg        *tgbotapi.Message
		text          string
		shouldTrigger bool
		descContains  string
	}{
		{
			name: "Custom sticker with thumbs up emoji (message 66607)",
			rawMsg: &tgbotapi.Message{
				MessageID: 66607,
				Sticker: &tgbotapi.Sticker{
					FileID:  "sticker_123",
					Emoji:   "👍",
					SetName: "custom_pack_123",
				},
			},
			text:          "[Sticker 👍]",
			shouldTrigger: true,
			descContains:  "👍",
		},
		{
			name: "Custom sticker with in-app editor (empty SetName)",
			rawMsg: &tgbotapi.Message{
				MessageID: 66608,
				Sticker: &tgbotapi.Sticker{
					FileID:  "sticker_standalone",
					Emoji:   "👍",
					SetName: "",
				},
			},
			text:          "[Sticker 👍]",
			shouldTrigger: true,
			descContains:  "👍",
		},
		{
			name: "Custom sticker without emoji",
			rawMsg: &tgbotapi.Message{
				MessageID: 66609,
				Sticker: &tgbotapi.Sticker{
					FileID:  "sticker_no_emoji",
					Emoji:   "",
					SetName: "spam_set",
				},
			},
			text:          "[Sticker]",
			shouldTrigger: true,
			descContains:  "spam_set",
		},
		{
			name:   "DB replay fallback with [Sticker 👍] format",
			rawMsg: nil,
			text:   "[Sticker 👍]",
			shouldTrigger: true,
			descContains:  "👍",
		},
		{
			name: "Custom emoji entity in message",
			rawMsg: &tgbotapi.Message{
				MessageID: 66610,
				Text:      "Look at this",
				Entities: []tgbotapi.MessageEntity{
					{Type: "custom_emoji"},
				},
			},
			text:          "Look at this",
			shouldTrigger: true,
			descContains:  "custom_emoji",
		},
		{
			name: "Normal text message",
			rawMsg: &tgbotapi.Message{
				MessageID: 66611,
				Text:      "Hello everyone, nice to meet you",
			},
			text:          "Hello everyone, nice to meet you",
			shouldTrigger: false,
		},
		{
			name: "Normal photo message",
			rawMsg: &tgbotapi.Message{
				MessageID: 66612,
				Photo:     []tgbotapi.PhotoSize{{FileID: "photo_123"}},
			},
			text:          "[Photo]",
			shouldTrigger: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &TriggerContext{
				RawMessage:       tc.rawMsg,
				Text:             tc.text,
				User:             user,
				UserMessageCount: 3,
			}
			if tc.rawMsg != nil {
				ctx.Message = &db.Message{
					MessageID: tc.rawMsg.MessageID,
					UserID:    user.UserID,
					Text:      tc.text,
				}
			}

			res, err := trig.Evaluate(ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Triggered != tc.shouldTrigger {
				t.Fatalf("expected triggered=%t, got %t (reason: %q)", tc.shouldTrigger, res.Triggered, res.Reason)
			}

			if tc.shouldTrigger {
				// Must contain kickban actions: delete message, ban user, deduct rep
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
					if act.Type == ActionAdjustReputation && act.RepDelta == -50 {
						hasRep = true
					}
				}
				if !hasDelete {
					t.Errorf("expected ActionDeleteMessage in actions: %+v", res.Actions)
				}
				if !hasBan {
					t.Errorf("expected ActionBanUser in actions: %+v", res.Actions)
				}
				if !hasRep {
					t.Errorf("expected ActionAdjustReputation with delta -50: %+v", res.Actions)
				}
			}
		})
	}
}

func TestCustomStickerTrigger_Exemptions(t *testing.T) {
	trig := NewCustomStickerTrigger(CustomStickerTriggerConfig{
		Enabled:       true,
		MaxReputation: 5,
		RepPenalty:    50,
	})

	stickerMsg := &tgbotapi.Message{
		MessageID: 70001,
		Sticker: &tgbotapi.Sticker{
			FileID:  "sticker_123",
			Emoji:   "👍",
			SetName: "custom_pack",
		},
	}

	t.Run("High reputation user (> MaxReputation) is exempt", func(t *testing.T) {
		trustedUser := &db.User{
			UserID:     11223344,
			Username:   "trusted_dev",
			Reputation: 50,
		}
		ctx := &TriggerContext{
			RawMessage: stickerMsg,
			Text:       "[Sticker 👍]",
			User:       trustedUser,
		}
		res, err := trig.Evaluate(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Triggered {
			t.Errorf("trusted user with rep=50 should NOT be triggered")
		}
	})

	t.Run("Whitelisted user (rep >= 100) is exempt", func(t *testing.T) {
		vipUser := &db.User{
			UserID:     998877,
			Username:   "vip_member",
			Reputation: 100,
		}
		ctx := &TriggerContext{
			RawMessage: stickerMsg,
			Text:       "[Sticker 👍]",
			User:       vipUser,
		}
		res, err := trig.Evaluate(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Triggered {
			t.Errorf("whitelisted user with rep=100 should NOT be triggered")
		}
	})

	t.Run("Admin user is exempt", func(t *testing.T) {
		adminUser := &db.User{
			UserID:     10104269,
			Username:   "group_admin",
			Reputation: 0,
			IsAdmin:    true,
		}
		ctx := &TriggerContext{
			RawMessage: stickerMsg,
			Text:       "[Sticker 👍]",
			User:       adminUser,
		}
		res, err := trig.Evaluate(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Triggered {
			t.Errorf("admin user should NOT be triggered")
		}
	})

	t.Run("Disabled trigger does not fire", func(t *testing.T) {
		disabledTrig := NewCustomStickerTrigger(CustomStickerTriggerConfig{
			Enabled: false,
		})
		lowRepUser := &db.User{
			UserID:     8927451894,
			Reputation: 0,
		}
		ctx := &TriggerContext{
			RawMessage: stickerMsg,
			Text:       "[Sticker 👍]",
			User:       lowRepUser,
		}
		res, err := disabledTrig.Evaluate(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Triggered {
			t.Errorf("disabled trigger should NOT fire")
		}
	})
}

func TestCustomStickerTrigger_AllowOfficialAnimatedEmoji(t *testing.T) {
	lowRepUser := &db.User{
		UserID:     8927451894,
		Reputation: 0,
	}

	officialAnimatedMsg := &tgbotapi.Message{
		MessageID: 80001,
		Sticker: &tgbotapi.Sticker{
			FileID:  "official_anim_123",
			Emoji:   "👍",
			SetName: "AnimatedEmojies",
		},
	}

	customMsg := &tgbotapi.Message{
		MessageID: 80002,
		Sticker: &tgbotapi.Sticker{
			FileID:  "custom_sticker_123",
			Emoji:   "👍",
			SetName: "custom_pack",
		},
	}

	t.Run("When AllowOfficialAnimatedEmoji=true, official AnimatedEmojies are allowed", func(t *testing.T) {
		trig := NewCustomStickerTrigger(CustomStickerTriggerConfig{
			Enabled:                    true,
			MaxReputation:              5,
			AllowOfficialAnimatedEmoji: true,
		})

		ctx := &TriggerContext{
			RawMessage: officialAnimatedMsg,
			Text:       "[Sticker 👍]",
			User:       lowRepUser,
		}
		res, err := trig.Evaluate(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Triggered {
			t.Errorf("expected official AnimatedEmojies NOT to be triggered when allowed")
		}

		// But custom sticker MUST still be triggered
		customCtx := &TriggerContext{
			RawMessage: customMsg,
			Text:       "[Sticker 👍]",
			User:       lowRepUser,
		}
		res2, err := trig.Evaluate(customCtx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res2.Triggered {
			t.Errorf("expected custom sticker to be triggered even when official animated emoji is allowed")
		}
	})

	t.Run("When AllowOfficialAnimatedEmoji=false, AnimatedEmojies is also kicked", func(t *testing.T) {
		trig := NewCustomStickerTrigger(CustomStickerTriggerConfig{
			Enabled:                    true,
			MaxReputation:              5,
			AllowOfficialAnimatedEmoji: false,
		})

		ctx := &TriggerContext{
			RawMessage: officialAnimatedMsg,
			Text:       "[Sticker 👍]",
			User:       lowRepUser,
		}
		res, err := trig.Evaluate(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.Triggered {
			t.Errorf("expected AnimatedEmojies to be kicked when AllowOfficialAnimatedEmoji is false")
		}
	})
}
