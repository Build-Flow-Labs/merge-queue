package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Build-Flow-Labs/merge-queue/internal/git"
	"github.com/Build-Flow-Labs/merge-queue/internal/github"
	gh "github.com/google/go-github/v60/github"
)

// Item represents a PR in the merge queue
type Item struct {
	ID             string     `json:"id"`
	InstallationID string     `json:"installation_id"`
	Owner          string     `json:"owner"`
	Repo           string     `json:"repo"`
	PRNumber       int        `json:"pr_number"`
	PRTitle        string     `json:"pr_title"`
	PRBranch       string     `json:"pr_branch"`
	BaseBranch     string     `json:"base_branch"`
	PRAuthor       string     `json:"pr_author"`
	Position       int        `json:"position"`
	Status         string     `json:"status"`
	ErrorMessage   *string    `json:"error_message,omitempty"`
	QueuedAt       time.Time  `json:"queued_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	QueuedBy       string     `json:"queued_by,omitempty"`
	RetryCount     int        `json:"retry_count"`
	MaxRetries     int        `json:"max_retries"`
	NextRetryAt    *time.Time `json:"next_retry_at,omitempty"`
}

// Settings represents per-repository merge queue settings
type Settings struct {
	ID                  string `json:"id"`
	InstallationID      string `json:"installation_id"`
	Owner               string `json:"owner"`
	Repo                string `json:"repo"`
	Enabled             bool   `json:"enabled"`
	MergeMethod         string `json:"merge_method"`
	RequireCIPass       bool   `json:"require_ci_pass"`
	AutoRebase          bool   `json:"auto_rebase"`
	DeleteBranchOnMerge bool   `json:"delete_branch_on_merge"`
	MaxQueueSize        int    `json:"max_queue_size"`
	CITimeoutMinutes    int    `json:"ci_timeout_minutes"`
	TriggerLabel        string `json:"trigger_label"`
	TriggerComment      string `json:"trigger_comment"`
}

// Processor handles background processing of the merge queue
type Processor struct {
	db         *sql.DB
	ghConfig   *github.AppConfig
	mu         sync.Mutex
	processing map[string]bool // owner/repo -> is processing
	stopChan   chan struct{}
	wg         sync.WaitGroup
}

// NewProcessor creates a new merge queue processor
func NewProcessor(db *sql.DB, ghConfig *github.AppConfig) *Processor {
	return &Processor{
		db:         db,
		ghConfig:   ghConfig,
		processing: make(map[string]bool),
		stopChan:   make(chan struct{}),
	}
}

// Start begins the background queue processor
func (p *Processor) Start() {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-p.stopChan:
				return
			case <-ticker.C:
				p.processQueues()
			}
		}
	}()
	log.Println("Merge queue processor started")
}

// Stop gracefully stops the processor
func (p *Processor) Stop() {
	close(p.stopChan)
	p.wg.Wait()
	log.Println("Merge queue processor stopped")
}

// processQueues finds all active queues and processes them
func (p *Processor) processQueues() {
	// First, check for failed items ready for auto-retry
	p.processRetries()

	rows, err := p.db.Query(`
		SELECT DISTINCT installation_id, owner, repo
		FROM merge_queue
		WHERE status IN ('queued', 'processing', 'rebasing', 'waiting_ci', 'merging')
	`)
	if err != nil {
		log.Printf("merge queue: failed to find active queues: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var installID, owner, repo string
		if err := rows.Scan(&installID, &owner, &repo); err != nil {
			continue
		}

		key := fmt.Sprintf("%s/%s", owner, repo)
		p.mu.Lock()
		if p.processing[key] {
			p.mu.Unlock()
			continue
		}
		p.processing[key] = true
		p.mu.Unlock()

		go func(installID, owner, repo, key string) {
			defer func() {
				p.mu.Lock()
				delete(p.processing, key)
				p.mu.Unlock()
			}()
			p.processQueue(installID, owner, repo)
		}(installID, owner, repo, key)
	}
}

// processQueue processes a single repository's queue
func (p *Processor) processQueue(installID, owner, repo string) {
	// First check for stuck items (in progress for too long) and reset them
	p.resetStuckItems(installID, owner, repo)

	// Get the first item that needs processing (queued or stuck in intermediate state)
	var item Item
	err := p.db.QueryRow(`
		SELECT id, installation_id, owner, repo, pr_number, pr_title, pr_branch, base_branch, pr_author, position, status, queued_at
		FROM merge_queue
		WHERE installation_id = $1 AND owner = $2 AND repo = $3
		  AND status IN ('queued', 'processing', 'rebasing', 'resolving_conflicts', 'waiting_ci', 'merging')
		ORDER BY
			CASE WHEN status = 'queued' THEN 1 ELSE 0 END,
			position ASC
		LIMIT 1
	`, installID, owner, repo).Scan(
		&item.ID, &item.InstallationID, &item.Owner, &item.Repo, &item.PRNumber,
		&item.PRTitle, &item.PRBranch, &item.BaseBranch, &item.PRAuthor, &item.Position, &item.Status, &item.QueuedAt,
	)
	if err == sql.ErrNoRows {
		return // No items to process
	}
	if err != nil {
		log.Printf("merge queue: failed to get queue item: %v", err)
		return
	}

	// If item was already being processed, log that we're resuming
	if item.Status != "queued" {
		log.Printf("merge queue: resuming PR #%d in %s/%s (was %s)", item.PRNumber, owner, repo, item.Status)
	}

	// Get settings
	settings := p.getSettings(installID, owner, repo)
	if !settings.Enabled {
		return
	}

	// Get GitHub installation ID
	var ghInstallID int64
	err = p.db.QueryRow(`SELECT github_installation_id FROM installations WHERE id = $1`, installID).Scan(&ghInstallID)
	if err != nil {
		log.Printf("merge queue: failed to get installation: %v", err)
		p.failItem(item.ID, "failed to get GitHub installation")
		return
	}

	// Create GitHub client
	ghClient, err := github.NewInstallationClient(p.ghConfig, ghInstallID)
	if err != nil {
		log.Printf("merge queue: failed to create GitHub client: %v", err)
		p.failItem(item.ID, fmt.Sprintf("failed to create GitHub client: %v", err))
		return
	}

	// Update status to processing
	p.updateStatus(item.ID, "processing")
	p.logEvent(item.ID, installID, owner, repo, item.PRNumber, "started", "", nil)

	ctx := context.Background()

	// Check if PR is still open
	pr, _, err := ghClient.PullRequests.Get(ctx, owner, repo, item.PRNumber)
	if err != nil {
		p.failItem(item.ID, fmt.Sprintf("failed to get PR: %v", err))
		return
	}

	if pr.GetState() != "open" {
		p.removeClosedPR(item.ID, installID, owner, repo, item.PRNumber, pr.GetState())
		return
	}

	// Check for merge conflicts
	mergeable := pr.GetMergeable()
	mergeableState := pr.GetMergeableState()

	if mergeableState == "dirty" || mergeable == false {
		// PR has conflicts - try to resolve
		p.updateStatus(item.ID, "resolving_conflicts")
		p.logEvent(item.ID, installID, owner, repo, item.PRNumber, "conflicts_detected", "", map[string]interface{}{
			"mergeable_state": mergeableState,
		})
		log.Printf("merge queue: PR #%d has conflicts, attempting to resolve", item.PRNumber)

		// Try GitHub API first (fast path for simple conflicts)
		_, _, err = ghClient.PullRequests.UpdateBranch(ctx, owner, repo, item.PRNumber, nil)
		if err != nil {
			// GitHub API couldn't resolve - try git-based resolution
			log.Printf("merge queue: GitHub API couldn't resolve conflicts, trying git-based resolution")

			// Get an access token for git operations
			token, tokenErr := github.GetInstallationToken(p.ghConfig, ghInstallID)
			if tokenErr != nil {
				p.failItem(item.ID, fmt.Sprintf("failed to get token for conflict resolution: %v", tokenErr))
				return
			}

			resolver := git.NewConflictResolver()
			result, resolveErr := resolver.Resolve(ctx, token, owner, repo, item.PRBranch, item.BaseBranch)

			if resolveErr != nil {
				p.failItem(item.ID, fmt.Sprintf("conflict resolution failed: %v", resolveErr))
				return
			}

			if !result.Success {
				p.logEvent(item.ID, installID, owner, repo, item.PRNumber, "conflicts_unresolved", "", map[string]interface{}{
					"unresolved_files": result.UnresolvedFiles,
					"resolved_files":   result.ResolvedFiles,
				})
				p.failItem(item.ID, fmt.Sprintf("conflicts require manual resolution: %s", result.Message))
				return
			}

			p.logEvent(item.ID, installID, owner, repo, item.PRNumber, "conflicts_resolved", "", map[string]interface{}{
				"method":         "git",
				"resolved_files": result.ResolvedFiles,
				"commit_sha":     result.CommitSHA,
			})
			log.Printf("merge queue: PR #%d conflicts resolved via git (%s)", item.PRNumber, result.Message)
		} else {
			p.logEvent(item.ID, installID, owner, repo, item.PRNumber, "conflicts_resolved", "", map[string]interface{}{
				"method": "github_api",
			})
			log.Printf("merge queue: PR #%d conflicts resolved via GitHub API", item.PRNumber)
		}

		time.Sleep(5 * time.Second) // Wait for CI to trigger after resolution

		// Re-fetch PR to get updated state
		pr, _, err = ghClient.PullRequests.Get(ctx, owner, repo, item.PRNumber)
		if err != nil {
			p.failItem(item.ID, fmt.Sprintf("failed to get PR after conflict resolution: %v", err))
			return
		}
	} else if settings.AutoRebase {
		// No conflicts, but auto-rebase is enabled - update branch anyway
		p.updateStatus(item.ID, "rebasing")
		_, _, err = ghClient.PullRequests.UpdateBranch(ctx, owner, repo, item.PRNumber, nil)
		if err != nil {
			if ghErr, ok := err.(*gh.ErrorResponse); ok && ghErr.Response.StatusCode == 422 {
				log.Printf("merge queue: PR #%d already up to date", item.PRNumber)
			} else {
				p.failItem(item.ID, fmt.Sprintf("failed to update branch: %v", err))
				return
			}
		} else {
			p.logEvent(item.ID, installID, owner, repo, item.PRNumber, "rebased", "", nil)
			time.Sleep(5 * time.Second) // Wait for CI to trigger
		}
	}

	// Wait for CI if required
	if settings.RequireCIPass {
		p.updateStatus(item.ID, "waiting_ci")

		timeout := time.Duration(settings.CITimeoutMinutes) * time.Minute
		deadline := time.Now().Add(timeout)

		var lastLoggedPending int
		for time.Now().Before(deadline) {
			// Use the new CI status checker that handles both Checks API and Status API
			ciStatus, err := github.GetCIStatus(ctx, ghClient, owner, repo, item.PRBranch)
			if err != nil {
				log.Printf("merge queue: failed to get CI status: %v", err)
				time.Sleep(30 * time.Second)
				continue
			}

			// If no CI checks exist, treat as success
			if ciStatus.TotalChecks == 0 {
				log.Printf("merge queue: PR #%d has no CI checks, proceeding", item.PRNumber)
				p.logEvent(item.ID, installID, owner, repo, item.PRNumber, "ci_passed", "", map[string]interface{}{"reason": "no_checks"})
				break
			}

			// Log runner detection on first check
			if ciStatus.HasSelfHosted {
				log.Printf("merge queue: PR #%d using self-hosted runners (%d self-hosted, %d github-hosted)",
					item.PRNumber, ciStatus.SelfHostedCount, ciStatus.HostedCount)
			}

			// Log progress periodically
			if ciStatus.PendingChecks != lastLoggedPending {
				log.Printf("merge queue: PR #%d CI status: %d/%d passed, %d pending, %d failed",
					item.PRNumber, ciStatus.PassedChecks, ciStatus.TotalChecks, ciStatus.PendingChecks, ciStatus.FailedChecks)
				lastLoggedPending = ciStatus.PendingChecks
			}

			if ciStatus.State == "success" {
				p.logEvent(item.ID, installID, owner, repo, item.PRNumber, "ci_passed", "", map[string]interface{}{
					"total_checks":    ciStatus.TotalChecks,
					"has_self_hosted": ciStatus.HasSelfHosted,
					"self_hosted":     ciStatus.SelfHostedCount,
					"github_hosted":   ciStatus.HostedCount,
				})
				break
			} else if ciStatus.State == "failure" {
				// Collect failed check names
				var failedNames []string
				for _, check := range ciStatus.Checks {
					if check.Conclusion == "failure" || check.Conclusion == "timed_out" || check.Conclusion == "cancelled" {
						failedNames = append(failedNames, check.Name)
					}
				}
				p.logEvent(item.ID, installID, owner, repo, item.PRNumber, "ci_failed", "", map[string]interface{}{
					"state":         ciStatus.State,
					"failed_checks": failedNames,
				})
				p.failItem(item.ID, fmt.Sprintf("CI failed: %v", failedNames))
				return
			}

			time.Sleep(30 * time.Second)
		}

		if time.Now().After(deadline) {
			p.failItem(item.ID, "CI timeout exceeded")
			return
		}
	}

	// Merge the PR
	p.updateStatus(item.ID, "merging")

	commitMsg := fmt.Sprintf("Merge PR #%d: %s", item.PRNumber, item.PRTitle)
	opts := &gh.PullRequestOptions{
		MergeMethod: settings.MergeMethod,
		CommitTitle: commitMsg,
	}

	result, _, err := ghClient.PullRequests.Merge(ctx, owner, repo, item.PRNumber, commitMsg, opts)
	if err != nil {
		p.failItem(item.ID, fmt.Sprintf("failed to merge: %v", err))
		return
	}

	if !result.GetMerged() {
		p.failItem(item.ID, "merge was not successful")
		return
	}

	// Success!
	now := time.Now()
	_, err = p.db.Exec(`
		UPDATE merge_queue
		SET status = 'merged', completed_at = $1
		WHERE id = $2
	`, now, item.ID)
	if err != nil {
		log.Printf("merge queue: failed to update merged status: %v", err)
	}

	p.logEvent(item.ID, installID, owner, repo, item.PRNumber, "merged", "", map[string]interface{}{
		"sha": result.GetSHA(),
	})

	// Delete branch if enabled
	if settings.DeleteBranchOnMerge {
		_, err = ghClient.Git.DeleteRef(ctx, owner, repo, "heads/"+item.PRBranch)
		if err != nil {
			log.Printf("merge queue: failed to delete branch %s: %v", item.PRBranch, err)
		}
	}

	log.Printf("merge queue: successfully merged PR #%d in %s/%s", item.PRNumber, owner, repo)

	// Reorder remaining queue
	p.reorderQueue(installID, owner, repo)
}

func (p *Processor) getSettings(installID, owner, repo string) *Settings {
	var s Settings
	err := p.db.QueryRow(`
		SELECT id, installation_id, owner, repo, enabled, merge_method, require_ci_pass, auto_rebase, delete_branch_on_merge, max_queue_size, ci_timeout_minutes, trigger_label, trigger_comment
		FROM repo_settings
		WHERE installation_id = $1 AND owner = $2 AND repo = $3
	`, installID, owner, repo).Scan(
		&s.ID, &s.InstallationID, &s.Owner, &s.Repo, &s.Enabled, &s.MergeMethod,
		&s.RequireCIPass, &s.AutoRebase, &s.DeleteBranchOnMerge, &s.MaxQueueSize, &s.CITimeoutMinutes,
		&s.TriggerLabel, &s.TriggerComment,
	)
	if err != nil {
		// Return defaults
		return &Settings{
			Enabled:             true,
			MergeMethod:         "merge",
			RequireCIPass:       true,
			AutoRebase:          true,
			DeleteBranchOnMerge: true,
			MaxQueueSize:        50,
			CITimeoutMinutes:    60,
			TriggerLabel:        "merge-queue",
			TriggerComment:      "/merge",
		}
	}
	return &s
}

func (p *Processor) updateStatus(itemID, status string) {
	now := time.Now()
	_, err := p.db.Exec(`
		UPDATE merge_queue
		SET status = $1, started_at = COALESCE(started_at, $2)
		WHERE id = $3
	`, status, now, itemID)
	if err != nil {
		log.Printf("merge queue: failed to update status: %v", err)
	}
}

func (p *Processor) failItem(itemID, errMsg string) {
	now := time.Now()

	// Get current retry count and max retries
	var retryCount, maxRetries int
	var installID, owner, repo string
	var prNumber int
	err := p.db.QueryRow(`
		SELECT installation_id, owner, repo, pr_number, retry_count, max_retries
		FROM merge_queue WHERE id = $1
	`, itemID).Scan(&installID, &owner, &repo, &prNumber, &retryCount, &maxRetries)
	if err != nil {
		log.Printf("merge queue: failed to get item for retry check: %v", err)
		return
	}

	// Increment retry count
	newRetryCount := retryCount + 1

	// Check if we should schedule a retry
	if newRetryCount < maxRetries && p.isRetryableError(errMsg) {
		// Exponential backoff: 1min, 2min, 4min
		backoff := time.Duration(1<<retryCount) * time.Minute
		nextRetry := now.Add(backoff)

		_, err = p.db.Exec(`
			UPDATE merge_queue
			SET status = 'failed',
			    error_message = $1,
			    retry_count = $2,
			    next_retry_at = $3,
			    started_at = NULL
			WHERE id = $4
		`, errMsg, newRetryCount, nextRetry, itemID)
		if err != nil {
			log.Printf("merge queue: failed to schedule retry: %v", err)
		}

		p.logEvent(itemID, installID, owner, repo, prNumber, "failed", "", map[string]interface{}{
			"error":         errMsg,
			"will_retry":    true,
			"retry_count":   newRetryCount,
			"next_retry_at": nextRetry.Format(time.RFC3339),
		})
		log.Printf("merge queue: item %s failed, will retry in %v (attempt %d/%d): %s", itemID, backoff, newRetryCount, maxRetries, errMsg)
	} else {
		// No more retries - mark as permanently failed
		_, err = p.db.Exec(`
			UPDATE merge_queue
			SET status = 'failed',
			    error_message = $1,
			    completed_at = $2,
			    retry_count = $3,
			    next_retry_at = NULL
			WHERE id = $4
		`, errMsg, now, newRetryCount, itemID)
		if err != nil {
			log.Printf("merge queue: failed to update failed status: %v", err)
		}

		p.logEvent(itemID, installID, owner, repo, prNumber, "failed", "", map[string]interface{}{
			"error":      errMsg,
			"will_retry": false,
			"final":      true,
		})
		log.Printf("merge queue: item %s permanently failed (no more retries): %s", itemID, errMsg)
	}
}

// isRetryableError determines if an error is worth retrying
func (p *Processor) isRetryableError(errMsg string) bool {
	// Don't retry user-facing issues that won't change
	permanentErrors := []string{
		"PR is no longer open",
		"merge was not successful",
		"CI failed with state",
	}
	for _, pe := range permanentErrors {
		if contains(errMsg, pe) {
			return false
		}
	}
	// Retry transient errors (API failures, timeouts, etc.)
	return true
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func (p *Processor) logEvent(itemID, installID, owner, repo string, prNumber int, eventType, actor string, details map[string]interface{}) {
	detailsJSON, _ := json.Marshal(details)
	_, err := p.db.Exec(`
		INSERT INTO queue_events (queue_item_id, installation_id, owner, repo, pr_number, event_type, actor, details)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, itemID, installID, owner, repo, prNumber, eventType, actor, detailsJSON)
	if err != nil {
		log.Printf("merge queue: failed to log event: %v", err)
	}
}

// resetStuckItems resets items that have been stuck in intermediate states for too long
func (p *Processor) resetStuckItems(installID, owner, repo string) {
	// Reset items stuck in intermediate states for more than 10 minutes
	result, err := p.db.Exec(`
		UPDATE merge_queue
		SET status = 'queued', started_at = NULL, error_message = NULL
		WHERE installation_id = $1 AND owner = $2 AND repo = $3
		  AND status IN ('processing', 'rebasing', 'resolving_conflicts')
		  AND started_at < NOW() - INTERVAL '10 minutes'
	`, installID, owner, repo)
	if err != nil {
		log.Printf("merge queue: failed to reset stuck items: %v", err)
		return
	}
	if rows, _ := result.RowsAffected(); rows > 0 {
		log.Printf("merge queue: reset %d stuck items in %s/%s", rows, owner, repo)
	}
}

func (p *Processor) reorderQueue(installID, owner, repo string) {
	_, err := p.db.Exec(`
		WITH ordered AS (
			SELECT id, ROW_NUMBER() OVER (ORDER BY position) as new_pos
			FROM merge_queue
			WHERE installation_id = $1 AND owner = $2 AND repo = $3 AND status = 'queued'
		)
		UPDATE merge_queue SET position = ordered.new_pos
		FROM ordered
		WHERE merge_queue.id = ordered.id
	`, installID, owner, repo)
	if err != nil {
		log.Printf("merge queue: failed to reorder queue: %v", err)
	}
}

// removeClosedPR removes a PR from the queue when it's no longer open
func (p *Processor) removeClosedPR(itemID, installID, owner, repo string, prNumber int, prState string) {
	now := time.Now()

	// If PR was merged externally, mark as merged; otherwise cancelled
	status := "cancelled"
	if prState == "merged" {
		status = "merged"
	}

	_, err := p.db.Exec(`
		UPDATE merge_queue
		SET status = $1, completed_at = $2, error_message = NULL
		WHERE id = $3
	`, status, now, itemID)
	if err != nil {
		log.Printf("merge queue: failed to remove closed PR: %v", err)
	}

	p.logEvent(itemID, installID, owner, repo, prNumber, status, "", map[string]interface{}{
		"reason":   "pr_closed",
		"pr_state": prState,
	})
	log.Printf("merge queue: removed PR #%d from queue (PR state: %s)", prNumber, prState)
}

// processRetries checks for failed items that are ready for auto-retry
func (p *Processor) processRetries() {
	rows, err := p.db.Query(`
		SELECT id, installation_id, owner, repo, pr_number, retry_count
		FROM merge_queue
		WHERE status = 'failed'
		  AND next_retry_at IS NOT NULL
		  AND next_retry_at <= NOW()
		  AND retry_count < max_retries
	`)
	if err != nil {
		log.Printf("merge queue: failed to find items for retry: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, installID, owner, repo string
		var prNumber, retryCount int
		if err := rows.Scan(&id, &installID, &owner, &repo, &prNumber, &retryCount); err != nil {
			continue
		}

		// Reset to queued status for retry
		_, err := p.db.Exec(`
			UPDATE merge_queue
			SET status = 'queued',
			    started_at = NULL,
			    error_message = NULL,
			    next_retry_at = NULL
			WHERE id = $1
		`, id)
		if err != nil {
			log.Printf("merge queue: failed to reset item for retry: %v", err)
			continue
		}

		p.logEvent(id, installID, owner, repo, prNumber, "auto_retry", "", map[string]interface{}{
			"retry_count": retryCount + 1,
		})
		log.Printf("merge queue: auto-retrying PR #%d in %s/%s (attempt %d)", prNumber, owner, repo, retryCount+1)
	}
}
