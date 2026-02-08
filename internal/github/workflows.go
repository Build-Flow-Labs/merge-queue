package github

import (
	"context"
	"time"

	"github.com/google/go-github/v60/github"
)

// WorkflowRun represents a workflow run
type WorkflowRun struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Status       string    `json:"status"`       // queued, in_progress, completed
	Conclusion   string    `json:"conclusion"`   // success, failure, cancelled, skipped, etc.
	Event        string    `json:"event"`        // push, pull_request, schedule, workflow_dispatch
	Branch       string    `json:"branch"`
	CommitSHA    string    `json:"commit_sha"`
	CommitMsg    string    `json:"commit_message"`
	Actor        string    `json:"actor"`
	RepoName     string    `json:"repo_name"`
	HTMLURL      string    `json:"html_url"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	RunStartedAt time.Time `json:"run_started_at,omitempty"`
}

// ListOrgWorkflowRuns lists recent workflow runs across all repos in an org
func ListOrgWorkflowRuns(ctx context.Context, client *github.Client, org string, repos []string, limit int) ([]WorkflowRun, error) {
	var allRuns []WorkflowRun

	for _, repo := range repos {
		if len(allRuns) >= limit {
			break
		}

		runs, _, err := client.Actions.ListRepositoryWorkflowRuns(ctx, org, repo, &github.ListWorkflowRunsOptions{
			ListOptions: github.ListOptions{PerPage: 20},
		})
		if err != nil {
			continue // Skip repos we can't access
		}

		for _, run := range runs.WorkflowRuns {
			if len(allRuns) >= limit {
				break
			}

			wr := WorkflowRun{
				ID:         run.GetID(),
				Name:       run.GetName(),
				Status:     run.GetStatus(),
				Conclusion: run.GetConclusion(),
				Event:      run.GetEvent(),
				Branch:     run.GetHeadBranch(),
				CommitSHA:  run.GetHeadSHA()[:7],
				Actor:      run.GetActor().GetLogin(),
				RepoName:   repo,
				HTMLURL:    run.GetHTMLURL(),
				CreatedAt:  run.GetCreatedAt().Time,
				UpdatedAt:  run.GetUpdatedAt().Time,
			}

			if run.HeadCommit != nil {
				msg := run.HeadCommit.GetMessage()
				if len(msg) > 60 {
					msg = msg[:57] + "..."
				}
				wr.CommitMsg = msg
			}

			if run.RunStartedAt != nil {
				wr.RunStartedAt = run.RunStartedAt.Time
			}

			allRuns = append(allRuns, wr)
		}
	}

	// Sort by created_at descending (most recent first)
	for i := 0; i < len(allRuns)-1; i++ {
		for j := i + 1; j < len(allRuns); j++ {
			if allRuns[j].CreatedAt.After(allRuns[i].CreatedAt) {
				allRuns[i], allRuns[j] = allRuns[j], allRuns[i]
			}
		}
	}

	if len(allRuns) > limit {
		allRuns = allRuns[:limit]
	}

	return allRuns, nil
}
