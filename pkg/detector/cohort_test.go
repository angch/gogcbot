package detector

import (
	"testing"

	"github.com/angch/gogcbot/pkg/db"
)

func TestMatchesCohort(t *testing.T) {
	tests := []struct {
		name          string
		ctx           *TriggerContext
		minHighUserID int64
		maxRep        int
		maxPosts      int
		want          bool
	}{
		{
			name: "valid new high-ID low-rep user",
			ctx: &TriggerContext{
				IsNewUser: true,
				User:      &db.User{UserID: 5000000000, Reputation: 0},
			},
			want: true,
		},
		{
			name: "nil context",
			ctx:  nil,
			want: false,
		},
		{
			name: "nil user in context",
			ctx: &TriggerContext{
				IsNewUser: true,
			},
			want: false,
		},
		{
			name: "admin user exempt",
			ctx: &TriggerContext{
				IsNewUser: true,
				User:      &db.User{UserID: 5000000000, Reputation: 0, IsAdmin: true},
			},
			want: false,
		},
		{
			name: "high reputation (>= 100) whitelist exempt",
			ctx: &TriggerContext{
				IsNewUser: true,
				User:      &db.User{UserID: 5000000000, Reputation: 100},
			},
			want: false,
		},
		{
			name: "reputation exceeds threshold",
			ctx: &TriggerContext{
				IsNewUser: true,
				User:      &db.User{UserID: 5000000000, Reputation: 10},
			},
			maxRep: 5,
			want:   false,
		},
		{
			name: "low user ID",
			ctx: &TriggerContext{
				IsNewUser: true,
				User:      &db.User{UserID: 123456, Reputation: 0},
			},
			minHighUserID: 1000000000,
			want:          false,
		},
		{
			name: "established user with too many posts",
			ctx: &TriggerContext{
				IsNewUser:        false,
				UserMessageCount: 15,
				User:             &db.User{UserID: 5000000000, Reputation: 0},
			},
			maxPosts: 5,
			want:     false,
		},
		{
			name: "established user within post threshold",
			ctx: &TriggerContext{
				IsNewUser:        false,
				UserMessageCount: 3,
				User:             &db.User{UserID: 5000000000, Reputation: 0},
			},
			maxPosts: 5,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchesCohort(tt.ctx, tt.minHighUserID, tt.maxRep, tt.maxPosts)
			if got != tt.want {
				t.Errorf("MatchesCohort() = %v, want %v", got, tt.want)
			}
		})
	}
}
