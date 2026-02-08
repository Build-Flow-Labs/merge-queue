package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/Build-Flow-Labs/merge-queue/internal/github"
	"github.com/Build-Flow-Labs/merge-queue/internal/queue"
	gh "github.com/google/go-github/v60/github"
)

// GitHubWebhook handles incoming GitHub App webhook events
func (h *Handlers) GitHubWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB max
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	// Verify webhook signature
	if h.ghConfig.WebhookSecret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if !verifySignature(body, sig, h.ghConfig.WebhookSecret) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	event := r.Header.Get("X-GitHub-Event")
	switch event {
	case "installation":
		h.handleInstallation(w, body)
	case "pull_request":
		h.handlePullRequest(w, body)
	case "issue_comment":
		h.handleIssueComment(w, body)
	case "ping":
		writeJSON(w, http.StatusOK, map[string]string{"status": "pong"})
	default:
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "event": event})
	}
}

func verifySignature(payload []byte, sig, secret string) bool {
	if !strings.HasPrefix(sig, "sha256=") {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

type installationEvent struct {
	Action       string `json:"action"`
	Installation struct {
		ID      int64 `json:"id"`
		Account struct {
			Login string `json:"login"`
			ID    int64  `json:"id"`
			Type  string `json:"type"`
		} `json:"account"`
	} `json:"installation"`
}

func (h *Handlers) handleInstallation(w http.ResponseWriter, body []byte) {
	var evt installationEvent
	if err := json.Unmarshal(body, &evt); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("installation event: %s for %s", evt.Action, evt.Installation.Account.Login)

	switch evt.Action {
	case "created":
		_, err := h.db.Exec(`
			INSERT INTO installations (github_installation_id, owner_type, owner_login, owner_id)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (github_installation_id) DO UPDATE SET updated_at = NOW()
		`, evt.Installation.ID, evt.Installation.Account.Type, evt.Installation.Account.Login, evt.Installation.Account.ID)
		if err != nil {
			log.Printf("failed to save installation: %v", err)
			http.Error(w, "failed to save installation", http.StatusInternalServerError)
			return
		}
	case "deleted":
		_, err := h.db.Exec(`DELETE FROM installations WHERE github_installation_id = $1`, evt.Installation.ID)
		if err != nil {
			log.Printf("failed to delete installation: %v", err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type pullRequestEvent struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		Title  string `json:"title"`
		State  string `json:"state"`
		Head   struct{ Ref string `json:"ref"` } `json:"head"`
		Base   struct{ Ref string `json:"ref"` } `json:"base"`
		User   struct{ Login string `json:"login"` } `json:"user"`
		Labels []struct{ Name string `json:"name"` } `json:"labels"`
	} `json:"pull_request"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct{ Login string `json:"login"` } `json:"owner"`
	} `json:"repository"`
	Installation struct{ ID int64 `json:"id"` } `json:"installation"`
	Sender       struct{ Login string `json:"login"` } `json:"sender"`
}

func (h *Handlers) handlePullRequest(w http.ResponseWriter, body []byte) {
	var evt pullRequestEvent
	if err := json.Unmarshal(body, &evt); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	owner := evt.Repository.Owner.Login
	repo := evt.Repository.Name

	// Get installation from DB
	var installID string
	err := h.db.QueryRow(`SELECT id FROM installations WHERE github_installation_id = $1`, evt.Installation.ID).Scan(&installID)
	if err != nil {
		log.Printf("installation not found: %v", err)
		writeJSON(w, http.StatusOK, map[string]string{"status": "installation not found"})
		return
	}

	// Get settings
	settings := h.getSettings(installID, owner, repo)

	// Auto-queue on PR opened or ready_for_review
	if evt.Action == "opened" || evt.Action == "ready_for_review" {
		log.Printf("Auto-queuing PR #%d in %s/%s (action: %s)", evt.Number, owner, repo, evt.Action)
		h.addToQueue(installID, owner, repo, evt.Number, evt.PullRequest.Title,
			evt.PullRequest.Head.Ref, evt.PullRequest.Base.Ref, evt.PullRequest.User.Login, evt.Sender.Login)
	}

	// Also queue on label (fallback)
	if evt.Action == "labeled" {
		for _, label := range evt.PullRequest.Labels {
			if label.Name == settings.TriggerLabel {
				h.addToQueue(installID, owner, repo, evt.Number, evt.PullRequest.Title,
					evt.PullRequest.Head.Ref, evt.PullRequest.Base.Ref, evt.PullRequest.User.Login, evt.Sender.Login)
				break
			}
		}
	}

	// Handle PR closed (merged or not)
	if evt.Action == "closed" {
		// Remove from queue if present
		h.db.Exec(`
			UPDATE merge_queue SET status = 'cancelled', completed_at = NOW()
			WHERE installation_id = $1 AND owner = $2 AND repo = $3 AND pr_number = $4 AND status = 'queued'
		`, installID, owner, repo, evt.Number)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type issueCommentEvent struct {
	Action  string `json:"action"`
	Issue   struct{ Number int `json:"number"` } `json:"issue"`
	Comment struct {
		Body string `json:"body"`
		User struct{ Login string `json:"login"` } `json:"user"`
	} `json:"comment"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct{ Login string `json:"login"` } `json:"owner"`
	} `json:"repository"`
	Installation struct{ ID int64 `json:"id"` } `json:"installation"`
}

func (h *Handlers) handleIssueComment(w http.ResponseWriter, body []byte) {
	var evt issueCommentEvent
	if err := json.Unmarshal(body, &evt); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if evt.Action != "created" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

	owner := evt.Repository.Owner.Login
	repo := evt.Repository.Name

	// Get installation
	var installID string
	err := h.db.QueryRow(`SELECT id FROM installations WHERE github_installation_id = $1`, evt.Installation.ID).Scan(&installID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "installation not found"})
		return
	}

	settings := h.getSettings(installID, owner, repo)

	// Check for trigger command
	comment := strings.TrimSpace(evt.Comment.Body)
	if comment == settings.TriggerComment || strings.HasPrefix(comment, settings.TriggerComment+" ") {
		// Need to get PR details from GitHub API
		// For now, queue with minimal info (processor will fetch details)
		h.addToQueue(installID, owner, repo, evt.Issue.Number, "", "", "", "", evt.Comment.User.Login)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handlers) getSettings(installID, owner, repo string) *queue.Settings {
	var s queue.Settings
	err := h.db.QueryRow(`
		SELECT id, installation_id, owner, repo, enabled, merge_method, require_ci_pass, auto_rebase, delete_branch_on_merge, max_queue_size, ci_timeout_minutes, trigger_label, trigger_comment
		FROM repo_settings
		WHERE installation_id = $1 AND owner = $2 AND repo = $3
	`, installID, owner, repo).Scan(
		&s.ID, &s.InstallationID, &s.Owner, &s.Repo, &s.Enabled, &s.MergeMethod,
		&s.RequireCIPass, &s.AutoRebase, &s.DeleteBranchOnMerge, &s.MaxQueueSize, &s.CITimeoutMinutes,
		&s.TriggerLabel, &s.TriggerComment,
	)
	if err != nil {
		return &queue.Settings{
			Enabled:        true,
			TriggerLabel:   "merge-queue",
			TriggerComment: "/merge",
		}
	}
	return &s
}

func (h *Handlers) addToQueue(installID, owner, repo string, prNumber int, title, branch, baseBranch, author, queuedBy string) {
	// Check if already queued
	var exists bool
	h.db.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM merge_queue WHERE installation_id = $1 AND owner = $2 AND repo = $3 AND pr_number = $4 AND status NOT IN ('merged', 'failed', 'cancelled'))
	`, installID, owner, repo, prNumber).Scan(&exists)
	if exists {
		return
	}

	// Get next position
	var maxPos sql.NullInt64
	h.db.QueryRow(`
		SELECT MAX(position) FROM merge_queue WHERE installation_id = $1 AND owner = $2 AND repo = $3 AND status = 'queued'
	`, installID, owner, repo).Scan(&maxPos)
	nextPos := 1
	if maxPos.Valid {
		nextPos = int(maxPos.Int64) + 1
	}

	// Default base branch
	if baseBranch == "" {
		baseBranch = "main"
	}

	_, err := h.db.Exec(`
		INSERT INTO merge_queue (installation_id, owner, repo, pr_number, pr_title, pr_branch, base_branch, pr_author, position, queued_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, installID, owner, repo, prNumber, title, branch, baseBranch, author, nextPos, queuedBy)
	if err != nil {
		log.Printf("failed to add to queue: %v", err)
	} else {
		log.Printf("Added PR #%d to queue for %s/%s at position %d", prNumber, owner, repo, nextPos)
	}
}

// API Handlers

func (h *Handlers) GetQueue(w http.ResponseWriter, r *http.Request) {
	owner := r.URL.Query().Get("owner")
	repo := r.URL.Query().Get("repo")

	if owner == "" || repo == "" {
		http.Error(w, "owner and repo parameters required", http.StatusBadRequest)
		return
	}

	rows, err := h.db.Query(`
		SELECT id, installation_id, owner, repo, pr_number, pr_title, pr_branch, base_branch, pr_author, position, status, error_message, queued_at, started_at, completed_at, retry_count, max_retries, next_retry_at
		FROM merge_queue
		WHERE owner = $1 AND repo = $2 AND status NOT IN ('merged', 'cancelled')
		ORDER BY position ASC
	`, owner, repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var items []queue.Item
	for rows.Next() {
		var item queue.Item
		var errMsg sql.NullString
		var startedAt, completedAt, nextRetryAt sql.NullTime
		if err := rows.Scan(
			&item.ID, &item.InstallationID, &item.Owner, &item.Repo, &item.PRNumber,
			&item.PRTitle, &item.PRBranch, &item.BaseBranch, &item.PRAuthor, &item.Position, &item.Status,
			&errMsg, &item.QueuedAt, &startedAt, &completedAt, &item.RetryCount, &item.MaxRetries, &nextRetryAt,
		); err != nil {
			continue
		}
		if errMsg.Valid {
			item.ErrorMessage = &errMsg.String
		}
		if startedAt.Valid {
			item.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			item.CompletedAt = &completedAt.Time
		}
		if nextRetryAt.Valid {
			item.NextRetryAt = &nextRetryAt.Time
		}
		items = append(items, item)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"queue": items,
		"count": len(items),
	})
}

func (h *Handlers) AddToQueue(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Owner    string `json:"owner"`
		Repo     string `json:"repo"`
		PRNumber int    `json:"pr_number"`
		Title    string `json:"title"`
		Branch   string `json:"branch"`
		Author   string `json:"author"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Find installation for this owner
	var installID string
	err := h.db.QueryRow(`SELECT id FROM installations WHERE owner_login = $1`, req.Owner).Scan(&installID)
	if err != nil {
		http.Error(w, "installation not found for owner", http.StatusNotFound)
		return
	}

	h.addToQueue(installID, req.Owner, req.Repo, req.PRNumber, req.Title, req.Branch, "main", req.Author, "api")
	writeJSON(w, http.StatusCreated, map[string]string{"status": "queued"})
}

func (h *Handlers) RemoveFromQueue(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("id")
	if itemID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	_, err := h.db.Exec(`
		UPDATE merge_queue SET status = 'cancelled', completed_at = NOW()
		WHERE id = $1 AND status IN ('queued', 'failed')
	`, itemID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (h *Handlers) RetryQueueItem(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("id")
	if itemID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	_, err := h.db.Exec(`
		UPDATE merge_queue SET status = 'queued', error_message = NULL, started_at = NULL, completed_at = NULL, retry_count = 0, next_retry_at = NULL
		WHERE id = $1 AND status IN ('failed', 'paused')
	`, itemID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
}

// PauseQueueItem pauses a queue item
func (h *Handlers) PauseQueueItem(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("id")
	if itemID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	_, err := h.db.Exec(`
		UPDATE merge_queue SET status = 'paused', error_message = NULL
		WHERE id = $1 AND status IN ('queued', 'failed')
	`, itemID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (h *Handlers) GetSettings(w http.ResponseWriter, r *http.Request) {
	owner := r.URL.Query().Get("owner")
	repo := r.URL.Query().Get("repo")

	if owner == "" || repo == "" {
		http.Error(w, "owner and repo parameters required", http.StatusBadRequest)
		return
	}

	var installID string
	err := h.db.QueryRow(`SELECT id FROM installations WHERE owner_login = $1`, owner).Scan(&installID)
	if err != nil {
		http.Error(w, "installation not found", http.StatusNotFound)
		return
	}

	settings := h.getSettings(installID, owner, repo)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings)
}

func (h *Handlers) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req queue.Settings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	var installID string
	err := h.db.QueryRow(`SELECT id FROM installations WHERE owner_login = $1`, req.Owner).Scan(&installID)
	if err != nil {
		http.Error(w, "installation not found", http.StatusNotFound)
		return
	}

	_, err = h.db.Exec(`
		INSERT INTO repo_settings (installation_id, owner, repo, enabled, merge_method, require_ci_pass, auto_rebase, delete_branch_on_merge, max_queue_size, ci_timeout_minutes, trigger_label, trigger_comment, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW())
		ON CONFLICT (installation_id, owner, repo)
		DO UPDATE SET enabled = $4, merge_method = $5, require_ci_pass = $6, auto_rebase = $7, delete_branch_on_merge = $8, max_queue_size = $9, ci_timeout_minutes = $10, trigger_label = $11, trigger_comment = $12, updated_at = NOW()
	`, installID, req.Owner, req.Repo, req.Enabled, req.MergeMethod, req.RequireCIPass, req.AutoRebase, req.DeleteBranchOnMerge, req.MaxQueueSize, req.CITimeoutMinutes, req.TriggerLabel, req.TriggerComment)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *Handlers) GetEvents(w http.ResponseWriter, r *http.Request) {
	owner := r.URL.Query().Get("owner")
	repo := r.URL.Query().Get("repo")

	query := `SELECT id, queue_item_id, owner, repo, pr_number, event_type, actor, details, created_at FROM queue_events WHERE 1=1`
	args := []interface{}{}
	argNum := 1

	if owner != "" && repo != "" {
		query += ` AND owner = $` + string(rune('0'+argNum)) + ` AND repo = $` + string(rune('0'+argNum+1))
		args = append(args, owner, repo)
		argNum += 2
	}
	query += ` ORDER BY created_at DESC LIMIT 100`

	rows, err := h.db.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Event struct {
		ID          string                 `json:"id"`
		QueueItemID *string                `json:"queue_item_id,omitempty"`
		Owner       string                 `json:"owner"`
		Repo        string                 `json:"repo"`
		PRNumber    int                    `json:"pr_number"`
		EventType   string                 `json:"event_type"`
		Actor       string                 `json:"actor,omitempty"`
		Details     map[string]interface{} `json:"details,omitempty"`
		CreatedAt   string                 `json:"created_at"`
	}

	var events []Event
	for rows.Next() {
		var e Event
		var queueItemID sql.NullString
		var actor sql.NullString
		var detailsJSON []byte
		var createdAt sql.NullTime
		if err := rows.Scan(&e.ID, &queueItemID, &e.Owner, &e.Repo, &e.PRNumber, &e.EventType, &actor, &detailsJSON, &createdAt); err != nil {
			continue
		}
		if queueItemID.Valid {
			e.QueueItemID = &queueItemID.String
		}
		if actor.Valid {
			e.Actor = actor.String
		}
		if len(detailsJSON) > 0 {
			json.Unmarshal(detailsJSON, &e.Details)
		}
		if createdAt.Valid {
			e.CreatedAt = createdAt.Time.Format("2006-01-02T15:04:05Z")
		}
		events = append(events, e)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"events": events,
		"count":  len(events),
	})
}

func (h *Handlers) ListInstallations(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(`SELECT id, github_installation_id, owner_type, owner_login, created_at FROM installations ORDER BY created_at DESC`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Installation struct {
		ID           string `json:"id"`
		GitHubID     int64  `json:"github_installation_id"`
		OwnerType    string `json:"owner_type"`
		OwnerLogin   string `json:"owner_login"`
		CreatedAt    string `json:"created_at"`
	}

	var installs []Installation
	for rows.Next() {
		var i Installation
		var createdAt sql.NullTime
		if err := rows.Scan(&i.ID, &i.GitHubID, &i.OwnerType, &i.OwnerLogin, &createdAt); err != nil {
			continue
		}
		if createdAt.Valid {
			i.CreatedAt = createdAt.Time.Format("2006-01-02T15:04:05Z")
		}
		installs = append(installs, i)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"installations": installs,
		"count":         len(installs),
	})
}

// GetCIStatus returns detailed CI status for a PR including runner information
func (h *Handlers) GetCIStatus(w http.ResponseWriter, r *http.Request) {
	owner := r.URL.Query().Get("owner")
	repo := r.URL.Query().Get("repo")
	ref := r.URL.Query().Get("ref")

	if owner == "" || repo == "" || ref == "" {
		http.Error(w, "owner, repo, and ref parameters required", http.StatusBadRequest)
		return
	}

	// Get installation for this owner
	var ghInstallID int64
	err := h.db.QueryRow(`SELECT github_installation_id FROM installations WHERE owner_login = $1`, owner).Scan(&ghInstallID)
	if err != nil {
		http.Error(w, "installation not found", http.StatusNotFound)
		return
	}

	// Create GitHub client
	ghClient, err := github.NewInstallationClient(h.ghConfig, ghInstallID)
	if err != nil {
		http.Error(w, "failed to create GitHub client", http.StatusInternalServerError)
		return
	}

	// Get CI status
	ciStatus, err := github.GetCIStatus(r.Context(), ghClient, owner, repo, ref)
	if err != nil {
		http.Error(w, "failed to get CI status: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ciStatus)
}

// ListRunners lists self-hosted runners for an organization
func (h *Handlers) ListRunners(w http.ResponseWriter, r *http.Request) {
	owner := r.URL.Query().Get("owner")
	if owner == "" {
		http.Error(w, "owner parameter required", http.StatusBadRequest)
		return
	}

	// Get installation for this owner
	var ghInstallID int64
	err := h.db.QueryRow(`SELECT github_installation_id FROM installations WHERE owner_login = $1`, owner).Scan(&ghInstallID)
	if err != nil {
		http.Error(w, "installation not found", http.StatusNotFound)
		return
	}

	// Create GitHub client
	ghClient, err := github.NewInstallationClient(h.ghConfig, ghInstallID)
	if err != nil {
		http.Error(w, "failed to create GitHub client", http.StatusInternalServerError)
		return
	}

	// Get repos to check for in-progress jobs
	var repos []string
	reposRes, _, err := ghClient.Apps.ListRepos(r.Context(), &gh.ListOptions{PerPage: 50})
	if err == nil {
		for _, repo := range reposRes.Repositories {
			repos = append(repos, repo.GetName())
		}
	}

	// List runners
	runners, err := github.ListOrgRunners(r.Context(), ghClient, owner, repos)
	if err != nil {
		http.Error(w, "failed to list runners: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"runners": runners,
		"count":   len(runners),
	})
}

// GetRunnerJobs gets recent jobs for a specific runner
func (h *Handlers) GetRunnerJobs(w http.ResponseWriter, r *http.Request) {
	owner := r.URL.Query().Get("owner")
	runnerName := r.URL.Query().Get("runner")
	if owner == "" || runnerName == "" {
		http.Error(w, "owner and runner parameters required", http.StatusBadRequest)
		return
	}

	// Get installation for this owner
	var ghInstallID int64
	err := h.db.QueryRow(`SELECT github_installation_id FROM installations WHERE owner_login = $1`, owner).Scan(&ghInstallID)
	if err != nil {
		http.Error(w, "installation not found", http.StatusNotFound)
		return
	}

	// Create GitHub client
	ghClient, err := github.NewInstallationClient(h.ghConfig, ghInstallID)
	if err != nil {
		http.Error(w, "failed to create GitHub client", http.StatusInternalServerError)
		return
	}

	// Get repos to search
	var repos []string
	reposRes, _, err := ghClient.Apps.ListRepos(r.Context(), &gh.ListOptions{PerPage: 50})
	if err == nil {
		for _, repo := range reposRes.Repositories {
			repos = append(repos, repo.GetName())
		}
	}

	// Get jobs for this runner
	jobs, err := github.GetRunnerJobs(r.Context(), ghClient, owner, runnerName, repos)
	if err != nil {
		http.Error(w, "failed to get runner jobs: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"jobs":  jobs,
		"count": len(jobs),
	})
}

// GetJobLogs gets logs for a specific job
func (h *Handlers) GetJobLogs(w http.ResponseWriter, r *http.Request) {
	owner := r.URL.Query().Get("owner")
	repo := r.URL.Query().Get("repo")
	jobID := r.URL.Query().Get("job_id")
	if owner == "" || repo == "" || jobID == "" {
		http.Error(w, "owner, repo, and job_id parameters required", http.StatusBadRequest)
		return
	}

	// Get installation for this owner
	var ghInstallID int64
	err := h.db.QueryRow(`SELECT github_installation_id FROM installations WHERE owner_login = $1`, owner).Scan(&ghInstallID)
	if err != nil {
		http.Error(w, "installation not found", http.StatusNotFound)
		return
	}

	// Create GitHub client
	ghClient, err := github.NewInstallationClient(h.ghConfig, ghInstallID)
	if err != nil {
		http.Error(w, "failed to create GitHub client", http.StatusInternalServerError)
		return
	}

	// Parse job ID
	var id int64
	fmt.Sscanf(jobID, "%d", &id)

	// Get logs
	logs, err := github.GetJobLogs(r.Context(), ghClient, owner, repo, id)
	if err != nil {
		http.Error(w, "failed to get job logs: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(logs))
}

// ListWorkflowRuns lists recent workflow runs across all repos
func (h *Handlers) ListWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	owner := r.URL.Query().Get("owner")
	status := r.URL.Query().Get("status") // optional: "in_progress" to filter
	if owner == "" {
		http.Error(w, "owner parameter required", http.StatusBadRequest)
		return
	}

	// Get installation for this owner
	var ghInstallID int64
	err := h.db.QueryRow(`SELECT github_installation_id FROM installations WHERE owner_login = $1`, owner).Scan(&ghInstallID)
	if err != nil {
		http.Error(w, "installation not found", http.StatusNotFound)
		return
	}

	// Create GitHub client
	ghClient, err := github.NewInstallationClient(h.ghConfig, ghInstallID)
	if err != nil {
		http.Error(w, "failed to create GitHub client", http.StatusInternalServerError)
		return
	}

	// Get repos - fetch more repos
	var repos []string
	opts := &gh.ListOptions{PerPage: 100}
	for {
		reposRes, resp, err := ghClient.Apps.ListRepos(r.Context(), opts)
		if err != nil {
			break
		}
		for _, repo := range reposRes.Repositories {
			repos = append(repos, repo.GetName())
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	var runs []github.WorkflowRun
	if status == "in_progress" {
		runs, err = github.ListInProgressWorkflowRuns(r.Context(), ghClient, owner, repos)
	} else {
		runs, err = github.ListOrgWorkflowRuns(r.Context(), ghClient, owner, repos, 100)
	}
	if err != nil {
		http.Error(w, "failed to list workflow runs: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"runs":  runs,
		"count": len(runs),
	})
}

// ListRepos lists repositories for an organization from GitHub
func (h *Handlers) ListRepos(w http.ResponseWriter, r *http.Request) {
	owner := r.URL.Query().Get("owner")
	if owner == "" {
		http.Error(w, "owner parameter required", http.StatusBadRequest)
		return
	}

	// Get installation for this owner
	var ghInstallID int64
	err := h.db.QueryRow(`SELECT github_installation_id FROM installations WHERE owner_login = $1`, owner).Scan(&ghInstallID)
	if err != nil {
		http.Error(w, "installation not found", http.StatusNotFound)
		return
	}

	// Create GitHub client
	ghClient, err := github.NewInstallationClient(h.ghConfig, ghInstallID)
	if err != nil {
		http.Error(w, "failed to create GitHub client", http.StatusInternalServerError)
		return
	}

	// List repos
	opts := &gh.ListOptions{PerPage: 100}
	var allRepos []string

	for {
		repos, resp, err := ghClient.Apps.ListRepos(r.Context(), opts)
		if err != nil {
			http.Error(w, "failed to list repos: "+err.Error(), http.StatusInternalServerError)
			return
		}

		for _, repo := range repos.Repositories {
			allRepos = append(allRepos, repo.GetName())
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"repos": allRepos,
		"count": len(allRepos),
	})
}
