package detector

import (
	"testing"

	"github.com/angch/gogcbot/pkg/db"
)

func TestHasRedPacketSuffix(t *testing.T) {
	tests := []struct {
		name      string
		fullName  string
		firstName string
		lastName  string
		want      bool
	}{
		{"Single first name with red packet", "张三🧧", "张三🧧", "", true},
		{"First and last name, ending with red packet", "张 三🧧", "张", "三🧧", true},
		{"Trailing space after red packet", "张三🧧 ", "张三🧧 ", "", true},
		{"Full name ends with red packet", "李雷🧧", "", "", true},
		{"No red packet", "张三", "张三", "", false},
		{"Red packet at start", "🧧张三", "🧧张三", "", false},
		{"Red packet in middle", "张🧧三", "张🧧三", "", false},
		{"Empty name", "", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasRedPacketSuffix(tt.fullName, tt.firstName, tt.lastName)
			if got != tt.want {
				t.Errorf("HasRedPacketSuffix(%q, %q, %q) = %v, want %v",
					tt.fullName, tt.firstName, tt.lastName, got, tt.want)
			}
		})
	}
}

func TestIsNameMostlyCJK(t *testing.T) {
	tests := []struct {
		name        string
		minRatio    float64
		minCJKChars int
		want        bool
	}{
		{"李雷🧧", 0.5, 1, true},
		{"兼职日结 🧧", 0.5, 1, true},
		{"🔥全网最高扶持🧧", 0.5, 1, true},
		{"John Doe🧧", 0.5, 1, false},
		{"Alex 李雷🧧", 0.5, 1, false},
		{"A李雷B🧧", 0.5, 1, true},
		{"李雷A🧧", 0.5, 1, true},
		{"🧧", 0.5, 1, false},
		{"🧧🧧", 0.5, 1, false},
		{"12345🧧", 0.5, 1, false},
		{"こんにちは🧧", 0.5, 1, true},
		{"안녕하세요🧧", 0.5, 1, true},
		{"李🧧", 0.5, 1, true},
		{"", 0.5, 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsNameMostlyCJK(tt.name, tt.minRatio, tt.minCJKChars)
			if got != tt.want {
				t.Errorf("IsNameMostlyCJK(%q, %v, %v) = %v, want %v",
					tt.name, tt.minRatio, tt.minCJKChars, got, tt.want)
			}
		})
	}
}

func TestIsMixedCapsUsername(t *testing.T) {
	tests := []struct {
		username string
		minLen   int
		want     bool
	}{
		{"@cbzbQFLOuHNkJZ", 5, true},
		{"cbzbQFLOuHNkJZ", 5, true},
		{"@aBcDe", 5, true},
		{"@cbzbQFLO", 5, true},
		{"@AbCdEfGhIj", 5, true},
		{"@aBCdefGHI", 5, true},
		{"@xyzABCuvw", 5, true},
		{"@test_user", 5, false},
		{"@TESTUSER", 5, false},
		{"@User", 5, false},
		{"@Username", 5, false},
		{"@user_name", 5, false},
		{"@Johndoe1", 5, false},
		{"@John", 5, false},
		{"", 5, false},
	}

	for _, tt := range tests {
		t.Run(tt.username, func(t *testing.T) {
			got := IsMixedCapsUsername(tt.username, tt.minLen)
			if got != tt.want {
				t.Errorf("IsMixedCapsUsername(%q, %d) = %v, want %v",
					tt.username, tt.minLen, got, tt.want)
			}
		})
	}
}

func TestRedPacketNameTrigger_Evaluate(t *testing.T) {
	cfg := RedPacketNameTriggerConfig{
		Enabled:           true,
		MinHighUserID:     1000000000,
		MaxReputation:     5,
		MaxUserPosts:      5,
		MinUsernameLength: 5,
		MinCJKRatio:       0.5,
		MinCJKChars:       1,
		RepPenalty:        20,
	}
	trig := NewRedPacketNameTrigger(cfg)

	tests := []struct {
		name          string
		ctx           *TriggerContext
		wantTriggered bool
		wantActions   int
	}{
		{
			name: "Matching high-ID new user with CJK name ending in 🧧 and mixed caps username @cbzbQFLOuHNkJZ",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     6890123456,
					Username:   "cbzbQFLOuHNkJZ",
					FirstName:  "全网最高扶持🧧",
					Reputation: 0,
				},
			},
			wantTriggered: true,
			wantActions:   3,
		},
		{
			name: "Matching user on join with first and last name ending in 🧧",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     7890123456,
					Username:   "aBcDeF",
					FirstName:  "兼职",
					LastName:   "日结🧧",
					Reputation: 0,
				},
			},
			wantTriggered: true,
			wantActions:   3,
		},
		{
			name: "Low user ID should not trigger",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     123456,
					Username:   "cbzbQFLOuHNkJZ",
					FirstName:  "全网最高扶持🧧",
					Reputation: 0,
				},
			},
			wantTriggered: false,
			wantActions:   0,
		},
		{
			name: "High reputation user (> 5) should not trigger",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     6890123456,
					Username:   "cbzbQFLOuHNkJZ",
					FirstName:  "全网最高扶持🧧",
					Reputation: 50,
				},
			},
			wantTriggered: false,
			wantActions:   0,
		},
		{
			name: "Admin user should not trigger",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     6890123456,
					Username:   "cbzbQFLOuHNkJZ",
					FirstName:  "全网最高扶持🧧",
					Reputation: 0,
					IsAdmin:    true,
				},
			},
			wantTriggered: false,
			wantActions:   0,
		},
		{
			name: "User name without red packet emoji should not trigger",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     6890123456,
					Username:   "cbzbQFLOuHNkJZ",
					FirstName:  "全网最高扶持",
					Reputation: 0,
				},
			},
			wantTriggered: false,
			wantActions:   0,
		},
		{
			name: "User name with red packet but non-CJK English name should not trigger",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     6890123456,
					Username:   "cbzbQFLOuHNkJZ",
					FirstName:  "John Doe🧧",
					Reputation: 0,
				},
			},
			wantTriggered: false,
			wantActions:   0,
		},
		{
			name: "User with normal lowercase username should not trigger",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     6890123456,
					Username:   "johndoe123",
					FirstName:  "全网最高扶持🧧",
					Reputation: 0,
				},
			},
			wantTriggered: false,
			wantActions:   0,
		},
		{
			name: "User with standard capitalized username should not trigger",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     6890123456,
					Username:   "Johndoe",
					FirstName:  "全网最高扶持🧧",
					Reputation: 0,
				},
			},
			wantTriggered: false,
			wantActions:   0,
		},
		{
			name: "Old user with high post count (> 5) should not trigger",
			ctx: &TriggerContext{
				IsNewUser:        false,
				UserMessageCount: 15,
				User: &db.User{
					UserID:     6890123456,
					Username:   "cbzbQFLOuHNkJZ",
					FirstName:  "全网最高扶持🧧",
					Reputation: 0,
				},
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

func TestNewNewUserRedPacketTrigger_Constructor(t *testing.T) {
	cfg := RedPacketNameTriggerConfig{
		Enabled: true,
	}
	trig := NewNewUserRedPacketTrigger(cfg)
	if trig.ID() != "red_packet_cjk_name" {
		t.Errorf("expected ID 'red_packet_cjk_name', got %q", trig.ID())
	}
	if trig.Name() != "Red Packet CJK Name & Mixed-Caps Username Detection" {
		t.Errorf("unexpected name: %q", trig.Name())
	}
}
