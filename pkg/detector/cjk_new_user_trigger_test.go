package detector

import (
	"testing"

	"github.com/angch/gogcbot/pkg/db"
	lingua "github.com/pemistahl/lingua-go"
)

func TestNewUserCJKTrigger_Evaluate(t *testing.T) {
	ld := lingua.NewLanguageDetectorBuilder().FromAllLanguages().Build()
	cfg := NewUserCJKTriggerConfig{
		Enabled:       true,
		MinHighUserID: 1000000000,
		MaxReputation: 5,
		MaxUserPosts:  5,
		RepPenalty:    20,
	}
	trig := NewNewUserCJKTriggerWithDetector(cfg, ld)

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
			name: "Matching new high-ID user sending 六栢o壹天 phrase",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     6000000001,
					Reputation: 0,
				},
				Text: "六栢o壹天",
			},
			wantTriggered: true,
			wantActions:   3,
		},
		{
			name: "Matching high-ID user on 2nd post (IsNewUser: false, UserMessageCount: 2) with 1 rep sending 六栢o壹天",
			ctx: &TriggerContext{
				IsNewUser:        false,
				UserMessageCount: 2,
				User: &db.User{
					UserID:     8972972199,
					Reputation: 1,
				},
				Text: "六栢o壹天",
			},
			wantTriggered: true,
			wantActions:   3,
		},
		{
			name: "Matching high-ID user on 2nd post after empty first message sending syndicate scam message with contact",
			ctx: &TriggerContext{
				IsNewUser:        false,
				UserMessageCount: 2,
				User: &db.User{
					UserID:     6170094611,
					Reputation: 1,
				},
				Text: "油管联盟-fb联盟-外汇盘-币盘-商城盘-NFT盘-刷单盘-提供模特视频-可以挂自己地址和客服-联系; @ Ai16811",
			},
			wantTriggered: true,
			wantActions:   3,
		},
		{
			name: "Matching high-ID user who got Shieldy rep bonus (Reputation: 5, UserMessageCount: 2) sending CJK spam",
			ctx: &TriggerContext{
				IsNewUser:         false,
				UserMessageCount:  2,
				HasVerifiedNotBot: true,
				User: &db.User{
					UserID:     8887001007,
					Reputation: 5,
				},
				Text: "六栢o壹天",
			},
			wantTriggered: true,
			wantActions:   3,
		},
		{
			name: "Matching new high-ID user sending spam phrase with 六栢o壹天",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     6000000002,
					Reputation: 0,
				},
				Text: "兼职日结 六栢o壹天 联系TG",
			},
			wantTriggered: true,
			wantActions:   3,
		},
		{
			name: "Established user seen before with high post count (>5)",
			ctx: &TriggerContext{
				IsNewUser:        false,
				UserMessageCount: 15,
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
			name: "High rep user (> max_reputation 5)",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     5000000000,
					Reputation: 50,
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
			name: "Japanese message from new high-ID user",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     7778889999,
					Reputation: 0,
				},
				Text: "こんにちは世界",
			},
			wantTriggered: true,
			wantActions:   3,
		},
		{
			name: "Korean message from new high-ID user",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     7778889998,
					Reputation: 0,
				},
				Text: "안녕하세요 반갑습니다",
			},
			wantTriggered: true,
			wantActions:   3,
		},
		{
			name: "Shieldy verification text (I am not a bot) should not trigger CJK ban",
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
			name: "English message with ideographic space and kaomoji (message 66322) should not trigger CJK ban",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     6522656342,
					Reputation: 0,
				},
				Text: "Thank you in advance!\u3000(..◜ᴗ◝..)",
			},
			wantTriggered: false,
			wantActions:   0,
		},
		{
			name: "English message with lenticular brackets should not trigger CJK ban",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     6522656342,
					Reputation: 0,
				},
				Text: "【Announcement】 Hyprland gamepad idle inhibit tool in Rust",
			},
			wantTriggered: false,
			wantActions:   0,
		},
		{
			name: "Long English post with incidental CJK characters should be forgiven and not trigger",
			ctx: &TriggerContext{
				IsNewUser: true,
				User: &db.User{
					UserID:     6522656342,
					Reputation: 0,
				},
				Text: "Hi everyone! Here is a detailed report on Linux Wayland gamepad support. We tested multiple controllers including 8BitDo (八位堂) and verified near 0% CPU usage across our gaming test session on Arch Linux with hyprland. Please report any bugs or feedback!",
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

func TestNewUserCJKTrigger_IsOnlyCJK_And_ContainsCJK(t *testing.T) {
	ld := lingua.NewLanguageDetectorBuilder().FromAllLanguages().Build()
	cfg := NewUserCJKTriggerConfig{Enabled: true}
	trig := NewNewUserCJKTriggerWithDetector(cfg, ld)

	tests := []struct {
		text string
		want bool
	}{
		{"你好世界", true},
		{"六栢o壹天", true},
		{"六百一天", true},
		{"六佰一天", true},
		{"兼职日结 六栢o壹天", true},
		{"油管联盟-fb联盟-外汇盘-币盘-商城盘-NFT盘-刷单盘-提供模特视频-可以挂自己地址和客服-联系; @ Ai16811", true},
		{"你好！今天怎么样？", true},
		{"恭喜发财 123", true},
		{"無風險有想法的莱", true},
		{"演员来", true},
		{"Hello 你好", true},
		{"i test edge cases only. @hulksmashbannerbot 你是笨蛋吗？", true},
		{"こんにちは", true},
		{"カタカナ", true},
		{"안녕하세요", true},
		{"ㄅㄆㄇㄈ", true},
		{"English text only", false},
		{"123456", false},
		{"!@#$%^&*()_+", false},
		{"", false},
		{"Thank you in advance!\u3000(..◜ᴗ◝..)", false},
		{"\u3000", false},
		{"【Announcement】", false},
		{"Hello〜", false},
		{"(..◜ᴗ◝..)", false},
		{"Single char 中", false},
		{"Hi everyone! Here is a detailed report on Linux Wayland gamepad support. We tested multiple controllers including 8BitDo (八位堂) and verified near 0% CPU usage across our gaming test session on Arch Linux with hyprland. Please report any bugs or feedback!", false},
		{"恭喜发财 万事如意 兼职日结 六佰一天 真实靠谱 赶快联系客服 稳定出金 免费领红包 随时提现 欢迎加入我们的大家庭 享受更多福利待遇", true},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got := trig.ContainsCJK(tt.text)
			if got != tt.want {
				t.Errorf("ContainsCJK(%q) = %v, want %v", tt.text, got, tt.want)
			}
			gotOnly := trig.IsOnlyCJK(tt.text)
			if gotOnly != tt.want {
				t.Errorf("IsOnlyCJK(%q) = %v, want %v", tt.text, gotOnly, tt.want)
			}
			// Test backwards compatibility aliases
			gotChinese := trig.ContainsChinese(tt.text)
			if gotChinese != tt.want {
				t.Errorf("ContainsChinese(%q) = %v, want %v", tt.text, gotChinese, tt.want)
			}
			gotOnlyChinese := trig.IsOnlyChinese(tt.text)
			if gotOnlyChinese != tt.want {
				t.Errorf("IsOnlyChinese(%q) = %v, want %v", tt.text, gotOnlyChinese, tt.want)
			}
		})
	}
}

func TestBackwardsCompatibilityConstructors(t *testing.T) {
	cfg := NewUserChineseTriggerConfig{
		Enabled:         true,
		MinHighUserID:   1000000000,
		MaxReputation:   0,
		MinChineseChars: 1,
		RepPenalty:      20,
	}

	trig1 := NewNewUserChineseTrigger(cfg)
	if trig1.ID() != "new_user_cjk" {
		t.Errorf("Expected ID 'new_user_cjk', got %q", trig1.ID())
	}

	ld := lingua.NewLanguageDetectorBuilder().FromAllLanguages().Build()
	trig2 := NewNewUserChineseTriggerWithDetector(cfg, ld)
	if !trig2.ContainsCJK("六栢o壹天") {
		t.Errorf("Expected ContainsCJK to be true for 六栢o壹天")
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

func TestNewUserCJKTrigger_ForgivingForLongerMessages(t *testing.T) {
	ld := lingua.NewLanguageDetectorBuilder().FromAllLanguages().Build()
	cfg := NewUserCJKTriggerConfig{
		Enabled:       true,
		MinHighUserID: 1000000000,
		MaxReputation: 5,
		MaxUserPosts:  5,
		MinCJKChars:   2,
		MinCJKRatio:   0.15,
		RepPenalty:    20,
	}
	trig := NewNewUserCJKTriggerWithDetector(cfg, ld)

	tests := []struct {
		name          string
		text          string
		wantTriggered bool
	}{
		{
			name: "Noah's full original message (message 66322) - English post with kaomoji",
			text: "Hi everyone! Have you ever played a game on Linux with a controller but your Wayland session completely ignores its input and idles/sleeps/locks midgame? That happened to me on hyprland with hypridle. I just fixed that issue with a tool i created in Rust: \n\nhttps://gitlab.com/noahpolimon/gamepad-idle-inhibit\n\nI still am not fully sure it works correctly on all setup, or if i am the only one facing this problem at all. I could test it only with my 8Bit Ultimate 2C controller and it works perfectly so far with near 0% cpu and around 1-2MiB of memory.\n\nEven if you are not a Linux gamer and just interested in Rust / programming in general, I could use some feedback! If you do game on Linux and have this problem, I would appreciate if you start using and breaking this tool while reporting issues you might face. \nThank you in advance!　(..◜ᴗ◝..)",
			wantTriggered: false,
		},
		{
			name: "Long English message with incidental Japanese mention (東京)",
			text: "Hi everyone! Have you ever played a game on Linux with a controller but your Wayland session completely ignores its input and idles/sleeps/locks midgame? That happened to me on hyprland with hypridle. I just fixed that issue with a tool i created in Rust. We tested this during our trip to Tokyo (東京) and it worked smoothly across all devices.",
			wantTriggered: false,
		},
		{
			name: "Long English message with incidental Chinese input mention (测试)",
			text: "Here is a long message discussing some code in Go and how we handle Unicode characters. For example, testing the Chinese character 测试 and checking how it renders in the UI. Everything looks good and works as expected across all platforms. We encourage anyone interested in internationalization to help test.",
			wantTriggered: false,
		},
		{
			name: "Medium English message with single/few CJK characters (cjkCount < 5)",
			text: "I was reading about the new anime movie 君の名は and wanted to know if anyone here watched it on Linux via VLC or mpv?",
			wantTriggered: false,
		},
		{
			name: "Short message with lone single CJK char (cjkCount < minChars 2)",
			text: "Single char 中",
			wantTriggered: false,
		},
		{
			name: "Short spam message with high CJK ratio (六栢o壹天)",
			text: "六栢o壹天",
			wantTriggered: true,
		},
		{
			name: "Short mixed spam message (Hello 你好)",
			text: "Hello 你好",
			wantTriggered: true,
		},
		{
			name: "Medium spam message with contact info (油管联盟...)",
			text: "油管联盟-fb联盟-外汇盘-币盘-商城盘-NFT盘-刷单盘-提供模特视频-可以挂自己地址和客服-联系; @ Ai16811",
			wantTriggered: true,
		},
		{
			name: "Long Chinese spam advertisement (high CJK ratio)",
			text: "恭喜发财 万事如意 兼职日结 六佰一天 真实靠谱 赶快联系客服 稳定出金 免费领红包 随时提现 欢迎加入我们的大家庭 享受更多福利待遇 本团队诚信经营 长期招募代理 佣金丰厚 提现秒到账",
			wantTriggered: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trig.ContainsCJK(tt.text)
			if got != tt.wantTriggered {
				t.Errorf("ContainsCJK() = %v, want %v for text: %q", got, tt.wantTriggered, tt.text)
			}
		})
	}
}

