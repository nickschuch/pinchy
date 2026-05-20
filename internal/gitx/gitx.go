// Package gitx provides helpers for managing git worktrees on the host. All
// operations shell out to the host's git binary; no CGo or third-party git
// libraries are used.
package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ErrNoGit is returned when git is not found in PATH.
var ErrNoGit = errors.New("git not found in PATH")

// ErrNotARepo is returned when a path is not inside a git repository.
var ErrNotARepo = errors.New("not a git repository")

// git runs git with the supplied arguments, using dir as the working directory
// (-C dir). It returns combined stdout+stderr on error for useful diagnostics.
func git(dir string, args ...string) (string, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return "", ErrNoGit
	}
	allArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command(gitPath, allArgs...)
	out, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(out))
	if err != nil {
		return outStr, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, outStr)
	}
	return outStr, nil
}

// FindRepoRoot returns the absolute path of the git repository root that
// contains path. If path is not inside any git repository, (false, "") is
// returned without an error. ErrNoGit is returned if git is not installed.
func FindRepoRoot(path string) (root string, found bool, err error) {
	if _, lerr := exec.LookPath("git"); lerr != nil {
		return "", false, ErrNoGit
	}
	out, err := git(path, "rev-parse", "--show-toplevel")
	if err != nil {
		// Distinguish "not a repo" from other errors by inspecting the output.
		lower := strings.ToLower(out)
		if strings.Contains(lower, "not a git repository") ||
			strings.Contains(lower, "not a git repo") {
			return "", false, nil
		}
		return "", false, err
	}
	return out, true, nil
}

// BranchExists reports whether a local branch with the given name exists in
// the repository at repoRoot.
func BranchExists(repoRoot, branch string) (bool, error) {
	_, err := git(repoRoot, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err != nil {
		// Exit status 1 means the ref doesn't exist — not a real error.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// AddWorktree creates a new worktree at worktreePath for repoRoot, on a newly
// created branch named newBranch (branched from the current HEAD). It is an
// error if the branch already exists or if worktreePath already exists.
func AddWorktree(repoRoot, worktreePath, newBranch string) error {
	_, err := git(repoRoot, "worktree", "add", "-b", newBranch, worktreePath)
	return err
}

// RemoveWorktree unregisters and removes the worktree at worktreePath from the
// repository at repoRoot. It uses --force so partially-initialised worktrees
// are cleaned up too. If git worktree remove fails (e.g. because the source
// repo has moved), it falls back to os.RemoveAll + git worktree prune so the
// directory is still cleaned up.
func RemoveWorktree(repoRoot, worktreePath string) error {
	_, err := git(repoRoot, "worktree", "remove", "--force", worktreePath)
	if err != nil {
		// Fallback: remove the directory ourselves and prune dangling entries.
		removeErr := os.RemoveAll(worktreePath)
		_, pruneErr := git(repoRoot, "worktree", "prune")
		if removeErr != nil {
			return fmt.Errorf("removing worktree directory %q: %w", worktreePath, removeErr)
		}
		if pruneErr != nil {
			// Non-fatal: the worktree is already gone from disk.
			_ = pruneErr
		}
	}
	return nil
}

// DeleteBranch deletes the local branch branch from the repository at
// repoRoot. It uses -D (force delete) so branches with unmerged commits are
// deleted too. Returns nil if the branch does not exist.
func DeleteBranch(repoRoot, branch string) error {
	out, err := git(repoRoot, "branch", "-D", branch)
	if err != nil {
		lower := strings.ToLower(out)
		if strings.Contains(lower, "not found") ||
			strings.Contains(lower, "error: branch") {
			// Branch didn't exist — treat as success.
			return nil
		}
		return err
	}
	return nil
}

// WorktreePath returns the canonical path for a pinchy worktree inside repo.
// The worktree is placed under <repo>/.pinchy-worktrees/<envName>.
func WorktreePath(repoRoot, envName string) string {
	return repoRoot + "/.pinchy-worktrees/" + envName
}

// EnsureGitExclude appends pattern to <repoRoot>/.git/info/exclude if it is
// not already present. This keeps the .pinchy-worktrees directory out of git
// status output without modifying tracked files. The function is best-effort:
// errors are returned but callers may choose to log-and-continue.
func EnsureGitExclude(repoRoot, pattern string) error {
	excludeDir := repoRoot + "/.git/info"
	if err := os.MkdirAll(excludeDir, 0o755); err != nil {
		return fmt.Errorf("creating .git/info: %w", err)
	}
	excludePath := excludeDir + "/exclude"

	// Read existing content to check for the pattern.
	existing, _ := os.ReadFile(excludePath)
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(line) == pattern {
			return nil // already present
		}
	}

	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening .git/info/exclude: %w", err)
	}
	defer f.Close()

	// Ensure we're on a new line.
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		if _, err := fmt.Fprintln(f); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(f, pattern)
	return err
}
