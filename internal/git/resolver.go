package git

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ConflictResolver handles git-based conflict resolution
type ConflictResolver struct {
	workDir string
}

// ResolutionResult contains the outcome of conflict resolution
type ResolutionResult struct {
	Success          bool
	Message          string
	ResolvedFiles    []string
	UnresolvedFiles  []string
	CommitSHA        string
}

// NewConflictResolver creates a new resolver
func NewConflictResolver() *ConflictResolver {
	return &ConflictResolver{}
}

// Resolve attempts to resolve merge conflicts for a PR
func (r *ConflictResolver) Resolve(ctx context.Context, token, owner, repo, prBranch, baseBranch string) (*ResolutionResult, error) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "merge-queue-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	r.workDir = tmpDir
	repoURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git", token, owner, repo)

	// Clone with the PR branch directly
	log.Printf("conflict resolver: cloning %s/%s branch %s", owner, repo, prBranch)
	if err := r.run(ctx, "git", "clone", "-b", prBranch, "--filter=blob:none", repoURL, "."); err != nil {
		return nil, fmt.Errorf("failed to clone: %w", err)
	}

	// Configure git
	r.run(ctx, "git", "config", "user.email", "merge-queue[bot]@users.noreply.github.com")
	r.run(ctx, "git", "config", "user.name", "merge-queue[bot]")

	// Fetch the base branch
	log.Printf("conflict resolver: fetching base branch %s", baseBranch)
	if err := r.run(ctx, "git", "fetch", "origin", baseBranch+":"+baseBranch); err != nil {
		return nil, fmt.Errorf("failed to fetch base branch: %w", err)
	}

	// Try to merge base branch (using local ref we just fetched)
	log.Printf("conflict resolver: merging %s into %s", baseBranch, prBranch)
	mergeErr := r.run(ctx, "git", "merge", baseBranch, "--no-edit")

	if mergeErr == nil {
		// No conflicts - this shouldn't happen if we got here, but handle it
		sha, _ := r.getOutput(ctx, "git", "rev-parse", "HEAD")
		return &ResolutionResult{
			Success:   true,
			Message:   "merged without conflicts",
			CommitSHA: strings.TrimSpace(sha),
		}, nil
	}

	// Get list of conflicted files using git status
	conflictOutput, _ := r.getOutput(ctx, "git", "diff", "--name-only", "--diff-filter=U")
	conflictedFiles := []string{}
	for _, f := range strings.Split(strings.TrimSpace(conflictOutput), "\n") {
		if f != "" {
			conflictedFiles = append(conflictedFiles, f)
		}
	}

	// If no conflicts found via diff, also try git status
	if len(conflictedFiles) == 0 {
		statusOutput, _ := r.getOutput(ctx, "git", "status", "--porcelain")
		for _, line := range strings.Split(statusOutput, "\n") {
			if strings.HasPrefix(line, "UU ") || strings.HasPrefix(line, "AA ") || strings.HasPrefix(line, "DD ") {
				conflictedFiles = append(conflictedFiles, strings.TrimSpace(line[3:]))
			}
		}
	}

	log.Printf("conflict resolver: found %d conflicted files: %v", len(conflictedFiles), conflictedFiles)

	// If we have no conflicts detected but merge failed, something is wrong
	if len(conflictedFiles) == 0 {
		r.run(ctx, "git", "merge", "--abort")
		return nil, fmt.Errorf("merge failed but no conflicts detected - aborting")
	}

	var resolvedFiles []string
	var unresolvedFiles []string

	for _, file := range conflictedFiles {
		if file == "" {
			continue
		}

		resolved := false
		strategy := r.getResolutionStrategy(file)
		log.Printf("conflict resolver: file %s -> strategy %s", file, strategy)

		switch strategy {
		case "theirs":
			// Accept the base branch version (useful for lock files that should match base)
			if err := r.run(ctx, "git", "checkout", "--theirs", file); err == nil {
				r.run(ctx, "git", "add", file)
				resolved = true
				log.Printf("conflict resolver: resolved %s using 'theirs' strategy", file)
			}
		case "ours":
			// Keep the PR branch version
			if err := r.run(ctx, "git", "checkout", "--ours", file); err == nil {
				r.run(ctx, "git", "add", file)
				resolved = true
				log.Printf("conflict resolver: resolved %s using 'ours' strategy", file)
			}
		case "union":
			// Try to keep both versions (works for some file types)
			if err := r.resolveUnion(ctx, file); err == nil {
				resolved = true
				log.Printf("conflict resolver: resolved %s using 'union' strategy", file)
			}
		case "regenerate":
			// For files that can be regenerated (like go.sum)
			if err := r.regenerateFile(ctx, file); err == nil {
				resolved = true
				log.Printf("conflict resolver: resolved %s by regenerating", file)
			}
		case "manual":
			// Source code files require manual resolution
			log.Printf("conflict resolver: %s requires manual resolution", file)
			resolved = false
		}

		if resolved {
			resolvedFiles = append(resolvedFiles, file)
		} else {
			unresolvedFiles = append(unresolvedFiles, file)
		}
	}

	// If there are unresolved files, abort
	if len(unresolvedFiles) > 0 {
		r.run(ctx, "git", "merge", "--abort")
		return &ResolutionResult{
			Success:         false,
			Message:         fmt.Sprintf("could not auto-resolve conflicts in: %s", strings.Join(unresolvedFiles, ", ")),
			ResolvedFiles:   resolvedFiles,
			UnresolvedFiles: unresolvedFiles,
		}, nil
	}

	// All conflicts resolved - commit and push
	commitMsg := fmt.Sprintf("Merge branch '%s' into %s (auto-resolved by merge-queue)", baseBranch, prBranch)
	if err := r.run(ctx, "git", "commit", "-m", commitMsg); err != nil {
		return nil, fmt.Errorf("failed to commit merge: %w", err)
	}

	log.Printf("conflict resolver: pushing resolved changes")
	if err := r.run(ctx, "git", "push", "origin", prBranch); err != nil {
		return nil, fmt.Errorf("failed to push: %w", err)
	}

	sha, _ := r.getOutput(ctx, "git", "rev-parse", "HEAD")
	return &ResolutionResult{
		Success:       true,
		Message:       fmt.Sprintf("resolved %d conflicts", len(resolvedFiles)),
		ResolvedFiles: resolvedFiles,
		CommitSHA:     strings.TrimSpace(sha),
	}, nil
}

// getResolutionStrategy determines how to resolve conflicts for a file
func (r *ConflictResolver) getResolutionStrategy(file string) string {
	base := filepath.Base(file)
	ext := filepath.Ext(file)

	// Lock files - prefer base branch version (theirs) to stay in sync
	lockFiles := map[string]bool{
		"package-lock.json": true,
		"yarn.lock":         true,
		"pnpm-lock.yaml":    true,
		"Gemfile.lock":      true,
		"poetry.lock":       true,
		"Cargo.lock":        true,
		"composer.lock":     true,
	}
	if lockFiles[base] {
		return "theirs"
	}

	// go.sum can be regenerated
	if base == "go.sum" {
		return "regenerate"
	}

	// Generated files - prefer base branch
	generatedPatterns := []string{
		".generated.",
		"_generated.",
		".min.js",
		".min.css",
		"dist/",
		"build/",
	}
	for _, pattern := range generatedPatterns {
		if strings.Contains(file, pattern) {
			return "theirs"
		}
	}

	// Changelog/version files - try union (keep both additions)
	if base == "CHANGELOG.md" || base == "HISTORY.md" || base == "NEWS.md" {
		return "union"
	}

	// Config files that shouldn't have conflicts in normal workflow
	configExts := map[string]bool{
		".json": true,
		".yaml": true,
		".yml":  true,
		".toml": true,
	}
	if configExts[ext] && !lockFiles[base] {
		// For config files, we could try a smarter merge, but for now skip
		return "manual"
	}

	// Source code files - require manual resolution
	return "manual"
}

// resolveUnion tries to keep both versions of conflicting sections
func (r *ConflictResolver) resolveUnion(ctx context.Context, file string) error {
	content, err := os.ReadFile(filepath.Join(r.workDir, file))
	if err != nil {
		return err
	}

	// Simple union: remove conflict markers and keep both versions
	lines := strings.Split(string(content), "\n")
	var result []string

	for _, line := range lines {
		if strings.HasPrefix(line, "<<<<<<<") {
			continue
		}
		if strings.HasPrefix(line, "=======") {
			continue
		}
		if strings.HasPrefix(line, ">>>>>>>") {
			continue
		}
		result = append(result, line)
	}

	// Write resolved content
	if err := os.WriteFile(filepath.Join(r.workDir, file), []byte(strings.Join(result, "\n")), 0644); err != nil {
		return err
	}

	return r.run(ctx, "git", "add", file)
}

// regenerateFile regenerates files like go.sum
func (r *ConflictResolver) regenerateFile(ctx context.Context, file string) error {
	base := filepath.Base(file)

	switch base {
	case "go.sum":
		// Accept theirs first, then run go mod tidy
		r.run(ctx, "git", "checkout", "--theirs", file)
		r.run(ctx, "git", "add", file)

		// Run go mod tidy to regenerate
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()

		if err := r.run(ctx, "go", "mod", "tidy"); err != nil {
			// If go mod tidy fails, just accept theirs
			return nil
		}
		return r.run(ctx, "git", "add", file)
	}

	return fmt.Errorf("don't know how to regenerate %s", file)
}

// run executes a command in the work directory
func (r *ConflictResolver) run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = r.workDir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w (stderr: %s)", name, strings.Join(args, " "), err, stderr.String())
	}
	return nil
}

// getOutput executes a command and returns its output
func (r *ConflictResolver) getOutput(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = r.workDir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	out, err := cmd.Output()
	return string(out), err
}
