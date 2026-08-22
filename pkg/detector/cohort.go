package detector

// CohortConfig defines parameters for identifying brand-new, high-ID, low-reputation users.
type CohortConfig struct {
	MinHighUserID int64 `mapstructure:"min_high_user_id" yaml:"min_high_user_id"`
	MaxReputation int   `mapstructure:"max_reputation" yaml:"max_reputation"`
	MaxUserPosts  int   `mapstructure:"max_user_posts" yaml:"max_user_posts"`
}

// MatchesCohort evaluates whether the context belongs to the target new high-ID low-reputation cohort.
// It returns false if the user is nil, an admin, has reputation >= 100 (whitelisted),
// has reputation exceeding maxReputation, is an established user exceeding maxUserPosts,
// or has a user ID below minHighUserID.
func MatchesCohort(ctx *TriggerContext, minHighUserID int64, maxReputation, maxUserPosts int) bool {
	if ctx == nil || ctx.User == nil {
		return false
	}
	// Whitelist: Admins and high reputation users (>= 100) are exempt
	if ctx.User.IsAdmin || ctx.User.Reputation >= 100 {
		return false
	}

	maxRep := maxReputation
	if maxRep <= 0 {
		maxRep = 5
	}
	if ctx.User.Reputation > maxRep {
		return false
	}

	maxPosts := maxUserPosts
	if maxPosts <= 0 {
		maxPosts = 5
	}
	isNewUser := ctx.IsNewUser || (ctx.UserMessageCount <= maxPosts)
	if !isNewUser {
		return false
	}

	minHighID := minHighUserID
	if minHighID <= 0 {
		minHighID = 1000000000
	}
	if ctx.User.UserID < minHighID {
		return false
	}

	return true
}
