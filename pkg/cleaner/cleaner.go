package cleaner

import (
	"context"
	"log"
	"time"

	"github.com/angch/gogcbot/pkg/db"
)

type RetentionCleaner struct {
	db              *db.DB
	retentionDays   int
	maxPostsPerUser int
	interval        time.Duration
}

func NewRetentionCleaner(database *db.DB, intervalHours int) *RetentionCleaner {
	if intervalHours <= 0 {
		intervalHours = 1
	}
	return &RetentionCleaner{
		db:              database,
		retentionDays:   7,
		maxPostsPerUser: 50,
		interval:        time.Duration(intervalHours) * time.Hour,
	}
}

func (c *RetentionCleaner) RunOnce() (oldPruned int64, userPruned int64, err error) {
	oldPruned, err = c.db.PruneOldMessages(c.retentionDays)
	if err != nil {
		return 0, 0, err
	}

	userPruned, err = c.db.PruneUserPostHistory(c.maxPostsPerUser)
	if err != nil {
		return oldPruned, 0, err
	}

	return oldPruned, userPruned, nil
}

func (c *RetentionCleaner) Start(ctx context.Context) {
	log.Printf("[RetentionCleaner] Starting background cleaner ticker every %s (7-day logs, 50 posts/user)", c.interval)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	// Initial cleanup run
	if old, user, err := c.RunOnce(); err != nil {
		log.Printf("[RetentionCleaner] Initial cleanup error: %v", err)
	} else {
		log.Printf("[RetentionCleaner] Initial cleanup complete: %d old messages (>7d) purged, %d excess user posts (>50/user) purged", old, user)
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("[RetentionCleaner] Stopping background cleaner...")
			return
		case <-ticker.C:
			old, user, err := c.RunOnce()
			if err != nil {
				log.Printf("[RetentionCleaner] Cleanup error: %v", err)
			} else if old > 0 || user > 0 {
				log.Printf("[RetentionCleaner] Periodic cleanup complete: %d old messages (>7d) purged, %d excess user posts (>50/user) purged", old, user)
			}
		}
	}
}
