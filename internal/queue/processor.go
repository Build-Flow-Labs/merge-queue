package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

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
	// Get the first queued item
	var item Item
	err := p.db.QueryRow(`
		SELECT id, installation_id, owner, repo, pr_number, pr_title, pr_branch, base_branch, pr_author, position, status, queued_at
		FROM merge_queue
		WHERE installation_id = $1 AND owner = $2 AND repo = $3 AND status = 'queued'
		ORDER BY position ASC
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
		p.failItem(item.ID, "PR is no longer open")
		return
	}

	// Auto-rebase if enabled
	if settings.AutoRebase {
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

		for time.Now().Before(deadline) {
			combined, _, err := ghClient.Repositories.GetCombinedStatus(ctx, owner, repo, item.PRBranch, nil)
			if err != nil {
				log.Printf("merge queue: failed to get CI status: %v", err)
				time.Sleep(30 * time.Second)
				continue
			}

			state := combined.GetState()
			if state == "success" {
				p.logEvent(item.ID, installID, owner, repo, item.PRNumber, "ci_passed", "", nil)
				break
			} else if state == "failure" || state == "error" {
				p.logEvent(item.ID, installID, owner, repo, item.PRNumber, "ci_failed", "", map[string]interface{}{"state": state})
				p.failItem(item.ID, fmt.Sprintf("CI failed with state: %s", state))
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
	_, err := p.db.Exec(`
		UPDATE merge_queue
		SET status = 'failed', error_message = $1, completed_at = $2
		WHERE id = $3
	`, errMsg, now, itemID)
	if err != nil {
		log.Printf("merge queue: failed to update failed status: %v", err)
	}

	var installID, owner, repo string
	var prNumber int
	_ = p.db.QueryRow(`SELECT installation_id, owner, repo, pr_number FROM merge_queue WHERE id = $1`, itemID).Scan(&installID, &owner, &repo, &prNumber)
	p.logEvent(itemID, installID, owner, repo, prNumber, "failed", "", map[string]interface{}{"error": errMsg})

	log.Printf("merge queue: item %s failed: %s", itemID, errMsg)
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
