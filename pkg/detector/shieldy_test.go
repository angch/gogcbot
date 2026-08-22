package detector

import "testing"

func TestIsShieldyChallengeText(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "Standard Shieldy English message",
			text: "Hello, @spammer! Please, press the button below within the time amount specified, otherwise you will be kicked.",
			want: true,
		},
		{
			name: "Shieldy message lowercase without comma",
			text: "please, press the button below within the time amount specified, otherwise you will be kicked.",
			want: true,
		},
		{
			name: "Shieldy message with user full name",
			text: "Hello, John Doe! Please, press the button below within the time amount specified, otherwise you will be kicked.",
			want: true,
		},
		{
			name: "Short Shieldy button prompt",
			text: "Please, press the button below within the time amount specified",
			want: true,
		},
		{
			name: "Regular user chat message",
			text: "Hello everyone, glad to be here!",
			want: false,
		},
		{
			name: "Shieldy verification reply from user",
			text: "I am not a bot",
			want: false,
		},
		{
			name: "Empty string",
			text: "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsShieldyChallengeText(tt.text)
			if got != tt.want {
				t.Errorf("IsShieldyChallengeText(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestIsProfileTriggerID(t *testing.T) {
	if !IsProfileTriggerID("new_user_spam_bio") {
		t.Errorf("expected new_user_spam_bio to be profile trigger")
	}
	if !IsProfileTriggerID("profile_keyword_ban") {
		t.Errorf("expected profile_keyword_ban to be profile trigger")
	}
	if !IsProfileTriggerID("red_packet_name") {
		t.Errorf("expected red_packet_name to be profile trigger")
	}
	if !IsProfileTriggerID("username_anomaly") {
		t.Errorf("expected username_anomaly to be profile trigger")
	}
	if !IsProfileTriggerID("nonsense_bio") {
		t.Errorf("expected nonsense_bio to be profile trigger")
	}
	if IsProfileTriggerID("new_user_cjk") {
		t.Errorf("expected new_user_cjk to NOT be profile trigger (it is a message trigger)")
	}
}

func TestIsProfileBanReason(t *testing.T) {
	if !IsProfileBanReason("Detection trigger (new_user_spam_bio): spam in bio") {
		t.Errorf("expected true for spam bio ban reason")
	}
	if !IsProfileBanReason("Detection trigger (profile_keyword_ban): matched blocklist in profile") {
		t.Errorf("expected true for profile keyword ban reason")
	}
	if !IsProfileBanReason("Detection trigger (red_packet_name): red packet in name") {
		t.Errorf("expected true for red packet name reason")
	}
	if !IsProfileBanReason("Detection trigger on join: profile trigger fired") {
		t.Errorf("expected true for join reason")
	}
	if IsProfileBanReason("Detection trigger (new_user_cjk): message contained CJK") {
		t.Errorf("expected false for pure message CJK reason")
	}
}
