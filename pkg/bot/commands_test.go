package bot

import (
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name     string
		msg      *tgbotapi.Message
		wantCmd  string
		wantArgs string
	}{
		{
			name: "Standard slash command with args",
			msg: &tgbotapi.Message{
				Text: "/warn @user123 test reason",
				Entities: []tgbotapi.MessageEntity{
					{Type: "bot_command", Offset: 0, Length: 5},
				},
			},
			wantCmd:  "warn",
			wantArgs: "@user123 test reason",
		},
		{
			name: "Exclamation mark command",
			msg: &tgbotapi.Message{
				Text: "!rep +10",
			},
			wantCmd:  "rep",
			wantArgs: "+10",
		},
		{
			name: "Uppercase command",
			msg: &tgbotapi.Message{
				Text: "/STATUS",
				Entities: []tgbotapi.MessageEntity{
					{Type: "bot_command", Offset: 0, Length: 7},
				},
			},
			wantCmd:  "status",
			wantArgs: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCmd, gotArgs := parseCommand(tt.msg)
			if gotCmd != tt.wantCmd {
				t.Errorf("cmd = %q, want %q", gotCmd, tt.wantCmd)
			}
			if gotArgs != tt.wantArgs {
				t.Errorf("args = %q, want %q", gotArgs, tt.wantArgs)
			}
		})
	}
}

func TestParseRepArgs(t *testing.T) {
	tests := []struct {
		name         string
		args         string
		isReply      bool
		wantTarget   string
		wantAbsolute bool
		wantAbsVal   int
		wantHasDelta bool
		wantDeltaVal int
	}{
		{
			name:         "Relative negative delta with User ID",
			args:         "8841787542 -100",
			isReply:      false,
			wantTarget:   "8841787542",
			wantAbsolute: false,
			wantHasDelta: true,
			wantDeltaVal: -100,
		},
		{
			name:         "Absolute rep setting with User ID",
			args:         "8841787542 =0",
			isReply:      false,
			wantTarget:   "8841787542",
			wantAbsolute: true,
			wantAbsVal:   0,
			wantHasDelta: false,
		},
		{
			name:         "Absolute rep setting with username",
			args:         "@alice =50",
			isReply:      false,
			wantTarget:   "@alice",
			wantAbsolute: true,
			wantAbsVal:   50,
			wantHasDelta: false,
		},
		{
			name:         "Absolute rep setting for @pickfire =100",
			args:         "@pickfire =100",
			isReply:      false,
			wantTarget:   "@pickfire",
			wantAbsolute: true,
			wantAbsVal:   100,
			wantHasDelta: false,
		},
		{
			name:         "Absolute rep setting with spaces @pickfire = 100",
			args:         "@pickfire = 100",
			isReply:      false,
			wantTarget:   "@pickfire",
			wantAbsolute: true,
			wantAbsVal:   100,
			wantHasDelta: false,
		},
		{
			name:         "Reply with absolute rep setting",
			args:         "=0",
			isReply:      true,
			wantTarget:   "",
			wantAbsolute: true,
			wantAbsVal:   0,
			wantHasDelta: false,
		},
		{
			name:         "Reply with relative positive delta",
			args:         "+10",
			isReply:      true,
			wantTarget:   "",
			wantAbsolute: false,
			wantHasDelta: true,
			wantDeltaVal: 10,
		},
		{
			name:         "Query rep for user without value",
			args:         "8841787542",
			isReply:      false,
			wantTarget:   "8841787542",
			wantAbsolute: false,
			wantHasDelta: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTarget, gotAbs, gotAbsVal, gotHasDelta, gotDeltaVal := parseRepArgs(tt.args, tt.isReply)
			if gotTarget != tt.wantTarget {
				t.Errorf("target = %q, want %q", gotTarget, tt.wantTarget)
			}
			if gotAbs != tt.wantAbsolute {
				t.Errorf("isAbsolute = %v, want %v", gotAbs, tt.wantAbsolute)
			}
			if gotAbsVal != tt.wantAbsVal {
				t.Errorf("absVal = %d, want %d", gotAbsVal, tt.wantAbsVal)
			}
			if gotHasDelta != tt.wantHasDelta {
				t.Errorf("hasDelta = %v, want %v", gotHasDelta, tt.wantHasDelta)
			}
			if gotDeltaVal != tt.wantDeltaVal {
				t.Errorf("deltaVal = %d, want %d", gotDeltaVal, tt.wantDeltaVal)
			}
		})
	}
}
