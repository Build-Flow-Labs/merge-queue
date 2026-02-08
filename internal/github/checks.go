package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v60/github"
)

// CIStatus represents the combined status of all CI checks for a commit
type CIStatus struct {
	State           string        `json:"state"`            // pending, success, failure, error
	TotalChecks     int           `json:"total_checks"`
	PassedChecks    int           `json:"passed_checks"`
	FailedChecks    int           `json:"failed_checks"`
	PendingChecks   int           `json:"pending_checks"`
	Checks          []CheckDetail `json:"checks"`
	HasSelfHosted   bool          `json:"has_self_hosted"`
	SelfHostedCount int           `json:"self_hosted_count"`
	HostedCount     int           `json:"hosted_count"`
}

// CheckDetail represents a single CI check
type CheckDetail struct {
	Name       string `json:"name"`
	Status     string `json:"status"`      // queued, in_progress, completed
	Conclusion string `json:"conclusion"`  // success, failure, neutral, cancelled, skipped, timed_out, action_required
	RunnerType string `json:"runner_type"` // github-hosted, self-hosted, unknown
	RunnerName string `json:"runner_name,omitempty"`
	URL        string `json:"url,omitempty"`
}

// GetCIStatus fetches CI status from both the Checks API and Commit Status API
func GetCIStatus(ctx context.Context, client *github.Client, owner, repo, ref string) (*CIStatus, error) {
	status := &CIStatus{
		State:  "pending",
		Checks: []CheckDetail{},
	}

	// 1. Get Check Runs (GitHub Actions, third-party checks)
	checkRuns, _, err := client.Checks.ListCheckRunsForRef(ctx, owner, repo, ref, &github.ListCheckRunsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		// Non-fatal - continue with commit status
		fmt.Printf("merge queue: failed to get check runs: %v\n", err)
	} else {
		for _, run := range checkRuns.CheckRuns {
			detail := CheckDetail{
				Name:       run.GetName(),
				Status:     run.GetStatus(),
				Conclusion: run.GetConclusion(),
				URL:        run.GetHTMLURL(),
			}

			// Detect runner type from the check run
			detail.RunnerType, detail.RunnerName = detectRunnerType(run)

			if detail.RunnerType == "self-hosted" {
				status.HasSelfHosted = true
				status.SelfHostedCount++
			} else if detail.RunnerType == "github-hosted" {
				status.HostedCount++
			}

			status.Checks = append(status.Checks, detail)
			status.TotalChecks++

			if run.GetStatus() != "completed" {
				status.PendingChecks++
			} else {
				switch run.GetConclusion() {
				case "success", "neutral", "skipped":
					status.PassedChecks++
				case "failure", "timed_out", "cancelled", "action_required":
					status.FailedChecks++
				}
			}
		}
	}

	// 2. Get Commit Statuses (legacy status API)
	combined, _, err := client.Repositories.GetCombinedStatus(ctx, owner, repo, ref, nil)
	if err != nil {
		// Non-fatal - continue with what we have
		fmt.Printf("merge queue: failed to get combined status: %v\n", err)
	} else {
		for _, s := range combined.Statuses {
			// Check if this status is already covered by a check run
			if isStatusCoveredByCheck(s.GetContext(), status.Checks) {
				continue
			}

			detail := CheckDetail{
				Name:       s.GetContext(),
				Conclusion: s.GetState(), // pending, success, error, failure
				Status:     "completed",
				RunnerType: "external", // Legacy statuses are typically external services
				URL:        s.GetTargetURL(),
			}

			if s.GetState() == "pending" {
				detail.Status = "in_progress"
			}

			status.Checks = append(status.Checks, detail)
			status.TotalChecks++

			switch s.GetState() {
			case "pending":
				status.PendingChecks++
			case "success":
				status.PassedChecks++
			case "failure", "error":
				status.FailedChecks++
			}
		}
	}

	// Determine overall state
	status.State = determineOverallState(status)

	return status, nil
}

// detectRunnerType attempts to identify if a check ran on a self-hosted or GitHub-hosted runner
func detectRunnerType(run *github.CheckRun) (runnerType, runnerName string) {
	// Check the output for runner information
	if run.Output != nil && run.Output.Text != nil {
		text := strings.ToLower(*run.Output.Text)
		if strings.Contains(text, "self-hosted") {
			return "self-hosted", extractRunnerName(text)
		}
	}

	// Check run name for common self-hosted patterns
	name := strings.ToLower(run.GetName())
	if strings.Contains(name, "self-hosted") ||
		strings.Contains(name, "self_hosted") ||
		strings.Contains(name, "on-prem") ||
		strings.Contains(name, "internal-runner") {
		return "self-hosted", ""
	}

	// Check the app slug - github-actions is the GitHub Actions app
	if run.App != nil {
		slug := run.App.GetSlug()
		if slug == "github-actions" {
			// This is GitHub Actions - could be hosted or self-hosted
			// We'll need to check the workflow run for more details
			return detectRunnerFromDetails(run)
		}
	}

	return "unknown", ""
}

// detectRunnerFromDetails tries to determine runner type from check run details
func detectRunnerFromDetails(run *github.CheckRun) (string, string) {
	// Check the external ID which often contains runner info
	externalID := run.GetExternalID()
	if strings.Contains(strings.ToLower(externalID), "self-hosted") {
		return "self-hosted", ""
	}

	// Check annotations for runner labels
	// Self-hosted runners often have custom labels that show up in annotations

	// Default to github-hosted for GitHub Actions without clear self-hosted indicators
	return "github-hosted", ""
}

// extractRunnerName tries to extract the runner name from output text
func extractRunnerName(text string) string {
	// Look for patterns like "Runner: my-runner-name" or "self-hosted runner: xxx"
	patterns := []string{"runner:", "runner name:", "self-hosted:"}
	for _, pattern := range patterns {
		if idx := strings.Index(text, pattern); idx != -1 {
			// Extract the next word/phrase
			rest := text[idx+len(pattern):]
			rest = strings.TrimSpace(rest)
			if endIdx := strings.IndexAny(rest, " \n\t,"); endIdx != -1 {
				return rest[:endIdx]
			}
			if len(rest) > 0 && len(rest) < 50 {
				return rest
			}
		}
	}
	return ""
}

// isStatusCoveredByCheck checks if a commit status is already represented by a check run
func isStatusCoveredByCheck(context string, checks []CheckDetail) bool {
	contextLower := strings.ToLower(context)
	for _, check := range checks {
		if strings.ToLower(check.Name) == contextLower {
			return true
		}
		// Also check for common prefixes
		if strings.HasPrefix(strings.ToLower(check.Name), contextLower) {
			return true
		}
	}
	return false
}

// determineOverallState calculates the overall CI state from individual checks
func determineOverallState(status *CIStatus) string {
	if status.TotalChecks == 0 {
		return "success" // No checks = success (configurable behavior)
	}

	if status.FailedChecks > 0 {
		return "failure"
	}

	if status.PendingChecks > 0 {
		return "pending"
	}

	return "success"
}

// GetWorkflowRuns fetches workflow runs to get more detailed runner information
func GetWorkflowRuns(ctx context.Context, client *github.Client, owner, repo, branch string) ([]WorkflowRunInfo, error) {
	runs, _, err := client.Actions.ListWorkflowRunsByFileName(ctx, owner, repo, "", &github.ListWorkflowRunsOptions{
		Branch: branch,
		ListOptions: github.ListOptions{
			PerPage: 10,
		},
	})
	if err != nil {
		return nil, err
	}

	var info []WorkflowRunInfo
	for _, run := range runs.WorkflowRuns {
		wri := WorkflowRunInfo{
			ID:         run.GetID(),
			Name:       run.GetName(),
			Status:     run.GetStatus(),
			Conclusion: run.GetConclusion(),
			URL:        run.GetHTMLURL(),
		}

		// Get jobs for this run to find runner info
		jobs, _, err := client.Actions.ListWorkflowJobs(ctx, owner, repo, run.GetID(), &github.ListWorkflowJobsOptions{})
		if err == nil {
			for _, job := range jobs.Jobs {
				jobInfo := JobInfo{
					Name:       job.GetName(),
					Status:     job.GetStatus(),
					Conclusion: job.GetConclusion(),
				}

				// Check runner labels
				for _, label := range job.Labels {
					if label == "self-hosted" {
						jobInfo.RunnerType = "self-hosted"
						break
					}
					// Common GitHub-hosted labels
					if label == "ubuntu-latest" || label == "ubuntu-22.04" ||
						label == "macos-latest" || label == "windows-latest" ||
						strings.HasPrefix(label, "ubuntu-") ||
						strings.HasPrefix(label, "macos-") ||
						strings.HasPrefix(label, "windows-") {
						jobInfo.RunnerType = "github-hosted"
					}
				}

				// Get runner name if available
				if job.RunnerName != nil {
					jobInfo.RunnerName = *job.RunnerName
					// Self-hosted runners often have custom names
					if jobInfo.RunnerType == "" && !isGitHubHostedRunnerName(*job.RunnerName) {
						jobInfo.RunnerType = "self-hosted"
					}
				}

				if jobInfo.RunnerType == "" {
					jobInfo.RunnerType = "unknown"
				}

				wri.Jobs = append(wri.Jobs, jobInfo)
			}
		}

		info = append(info, wri)
	}

	return info, nil
}

// WorkflowRunInfo contains detailed workflow run information
type WorkflowRunInfo struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	URL        string    `json:"url"`
	Jobs       []JobInfo `json:"jobs"`
}

// JobInfo contains job-level information including runner details
type JobInfo struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	RunnerType string `json:"runner_type"`
	RunnerName string `json:"runner_name,omitempty"`
}

// isGitHubHostedRunnerName checks if a runner name matches GitHub-hosted patterns
func isGitHubHostedRunnerName(name string) bool {
	name = strings.ToLower(name)
	// GitHub-hosted runners have names like "GitHub Actions 2", "Hosted Agent", etc.
	patterns := []string{
		"github actions",
		"hosted agent",
		"ubuntu-",
		"macos-",
		"windows-",
	}
	for _, p := range patterns {
		if strings.Contains(name, p) {
			return true
		}
	}
	return false
}
