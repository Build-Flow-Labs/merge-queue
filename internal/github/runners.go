package github

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/google/go-github/v60/github"
)

// Runner represents a self-hosted runner
type Runner struct {
	ID         int64       `json:"id"`
	Name       string      `json:"name"`
	OS         string      `json:"os"`
	Status     string      `json:"status"` // online, offline
	Busy       bool        `json:"busy"`
	Labels     []string    `json:"labels"`
	CurrentJob *CurrentJob `json:"current_job,omitempty"`
}

// CurrentJob represents the job currently running on a runner
type CurrentJob struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	WorkflowName string `json:"workflow_name"`
	RepoName     string `json:"repo_name"`
	HTMLURL      string `json:"html_url"`
	StartedAt    string `json:"started_at"`
}

// RunnerJob represents a job that ran on a runner
type RunnerJob struct {
	ID           int64  `json:"id"`
	RunID        int64  `json:"run_id"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	Conclusion   string `json:"conclusion"`
	StartedAt    string `json:"started_at,omitempty"`
	CompletedAt  string `json:"completed_at,omitempty"`
	WorkflowName string `json:"workflow_name"`
	RepoName     string `json:"repo_name"`
	HTMLURL      string `json:"html_url"`
}

// ListOrgRunners lists all self-hosted runners for an organization
func ListOrgRunners(ctx context.Context, client *github.Client, org string, repos []string) ([]Runner, error) {
	runners, _, err := client.Actions.ListOrganizationRunners(ctx, org, &github.ListOptions{PerPage: 100})
	if err != nil {
		return nil, fmt.Errorf("failed to list org runners: %w", err)
	}

	// Get in-progress jobs to match with busy runners
	inProgressJobs := getInProgressJobs(ctx, client, org, repos)

	var result []Runner
	for _, r := range runners.Runners {
		labels := make([]string, 0, len(r.Labels))
		for _, l := range r.Labels {
			labels = append(labels, l.GetName())
		}

		runner := Runner{
			ID:     r.GetID(),
			Name:   r.GetName(),
			OS:     r.GetOS(),
			Status: r.GetStatus(),
			Busy:   r.GetBusy(),
			Labels: labels,
		}

		// If busy, find the current job
		if r.GetBusy() {
			if job, ok := inProgressJobs[r.GetName()]; ok {
				runner.CurrentJob = job
			}
		}

		result = append(result, runner)
	}

	return result, nil
}

// getInProgressJobs fetches in-progress jobs across repos and maps them to runner names
func getInProgressJobs(ctx context.Context, client *github.Client, org string, repos []string) map[string]*CurrentJob {
	jobs := make(map[string]*CurrentJob)

	for _, repo := range repos {
		// Get in-progress workflow runs
		runs, _, err := client.Actions.ListRepositoryWorkflowRuns(ctx, org, repo, &github.ListWorkflowRunsOptions{
			Status:      "in_progress",
			ListOptions: github.ListOptions{PerPage: 10},
		})
		if err != nil {
			continue
		}

		for _, run := range runs.WorkflowRuns {
			// Get jobs for this run
			runJobs, _, err := client.Actions.ListWorkflowJobs(ctx, org, repo, run.GetID(), &github.ListWorkflowJobsOptions{
				Filter: "latest",
			})
			if err != nil {
				continue
			}

			for _, job := range runJobs.Jobs {
				if job.GetStatus() == "in_progress" && job.RunnerName != nil {
					currentJob := &CurrentJob{
						ID:           job.GetID(),
						Name:         job.GetName(),
						WorkflowName: run.GetName(),
						RepoName:     repo,
						HTMLURL:      job.GetHTMLURL(),
					}
					if job.StartedAt != nil {
						currentJob.StartedAt = job.StartedAt.Format("2006-01-02T15:04:05Z")
					}
					jobs[*job.RunnerName] = currentJob
				}
			}
		}
	}

	return jobs
}

// ListRepoRunners lists all self-hosted runners for a repository
func ListRepoRunners(ctx context.Context, client *github.Client, owner, repo string) ([]Runner, error) {
	runners, _, err := client.Actions.ListRunners(ctx, owner, repo, &github.ListOptions{PerPage: 100})
	if err != nil {
		return nil, fmt.Errorf("failed to list repo runners: %w", err)
	}

	var result []Runner
	for _, r := range runners.Runners {
		labels := make([]string, 0, len(r.Labels))
		for _, l := range r.Labels {
			labels = append(labels, l.GetName())
		}

		result = append(result, Runner{
			ID:     r.GetID(),
			Name:   r.GetName(),
			OS:     r.GetOS(),
			Status: r.GetStatus(),
			Busy:   r.GetBusy(),
			Labels: labels,
		})
	}

	return result, nil
}

// GetRunnerJobs gets recent jobs that ran on a specific runner
func GetRunnerJobs(ctx context.Context, client *github.Client, owner string, runnerName string, repos []string) ([]RunnerJob, error) {
	var jobs []RunnerJob

	for _, repo := range repos {
		// Get recent workflow runs
		runs, _, err := client.Actions.ListRepositoryWorkflowRuns(ctx, owner, repo, &github.ListWorkflowRunsOptions{
			ListOptions: github.ListOptions{PerPage: 20},
		})
		if err != nil {
			continue // Skip repos we can't access
		}

		for _, run := range runs.WorkflowRuns {
			// Get jobs for this run
			runJobs, _, err := client.Actions.ListWorkflowJobs(ctx, owner, repo, run.GetID(), &github.ListWorkflowJobsOptions{
				Filter: "all",
			})
			if err != nil {
				continue
			}

			for _, job := range runJobs.Jobs {
				// Check if this job ran on the target runner
				if job.RunnerName != nil && *job.RunnerName == runnerName {
					rj := RunnerJob{
						ID:           job.GetID(),
						RunID:        run.GetID(),
						Name:         job.GetName(),
						Status:       job.GetStatus(),
						Conclusion:   job.GetConclusion(),
						WorkflowName: run.GetName(),
						RepoName:     repo,
						HTMLURL:      job.GetHTMLURL(),
					}
					if job.StartedAt != nil {
						rj.StartedAt = job.StartedAt.Format("2006-01-02T15:04:05Z")
					}
					if job.CompletedAt != nil {
						rj.CompletedAt = job.CompletedAt.Format("2006-01-02T15:04:05Z")
					}
					jobs = append(jobs, rj)
				}
			}
		}

		// Limit total jobs
		if len(jobs) >= 50 {
			break
		}
	}

	return jobs, nil
}

// GetJobLogs fetches logs for a specific workflow job
func GetJobLogs(ctx context.Context, client *github.Client, owner, repo string, jobID int64) (string, error) {
	url, _, err := client.Actions.GetWorkflowJobLogs(ctx, owner, repo, jobID, 2)
	if err != nil {
		return "", fmt.Errorf("failed to get job logs URL: %w", err)
	}

	// Fetch the logs
	resp, err := http.Get(url.String())
	if err != nil {
		return "", fmt.Errorf("failed to fetch logs: %w", err)
	}
	defer resp.Body.Close()

	// Read logs (limit to 1MB)
	logs, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("failed to read logs: %w", err)
	}

	return string(logs), nil
}

// GetWorkflowRunLogs fetches logs for an entire workflow run
func GetWorkflowRunLogs(ctx context.Context, client *github.Client, owner, repo string, runID int64) (string, error) {
	url, _, err := client.Actions.GetWorkflowRunLogs(ctx, owner, repo, runID, 2)
	if err != nil {
		return "", fmt.Errorf("failed to get run logs URL: %w", err)
	}

	// Fetch the logs (this returns a zip file URL)
	resp, err := http.Get(url.String())
	if err != nil {
		return "", fmt.Errorf("failed to fetch logs: %w", err)
	}
	defer resp.Body.Close()

	// For now, just return instruction to download
	// Full implementation would unzip and parse
	return fmt.Sprintf("Logs available at: %s", url.String()), nil
}
