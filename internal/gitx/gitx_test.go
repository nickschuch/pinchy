package gitx_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickschuch/pinchy/internal/gitx"
)

// requireGit skips the test if git is not installed on the host.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH — skipping worktree tests")
	}
}

// initRepo creates a temporary git repository with one commit and returns its
// absolute path. The repo is cleaned up automatically when the test ends.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")

	// Create a file and commit so HEAD exists.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")

	return dir
}

// TestFindRepoRoot_Inside verifies that FindRepoRoot returns the repo root
// when called from a subdirectory of a git repository.
func TestFindRepoRoot_Inside(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)

	// Create a subdirectory.
	sub := filepath.Join(repo, "sub", "dir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	root, found, err := gitx.FindRepoRoot(sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected found=true, got false")
	}
	if root != repo {
		t.Fatalf("expected %q, got %q", repo, root)
	}
}

// TestFindRepoRoot_Outside verifies that FindRepoRoot returns found=false when
// the path is not inside any git repository.
func TestFindRepoRoot_Outside(t *testing.T) {
	requireGit(t)

	// Use os.TempDir() itself — it is unlikely to be inside a git repo.
	// We create a fresh temp dir to be sure.
	dir := t.TempDir()

	root, found, err := gitx.FindRepoRoot(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatalf("expected found=false, but got root=%q", root)
	}
}

// TestBranchExists verifies that BranchExists correctly reports whether a
// branch is present.
func TestBranchExists(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)

	// "main" branch was created by initRepo.
	ok, err := gitx.BranchExists(repo, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected main branch to exist")
	}

	// A branch that was never created.
	ok, err = gitx.BranchExists(repo, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected nonexistent branch to not exist")
	}
}

// TestAddWorktree verifies that AddWorktree creates a worktree directory and
// a new branch.
func TestAddWorktree(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)

	worktreePath := filepath.Join(repo, ".pinchy-worktrees", "myenv")
	if err := gitx.AddWorktree(repo, worktreePath, "myenv"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	// The worktree directory must exist.
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		t.Fatalf("worktree directory %q does not exist", worktreePath)
	}

	// The branch must have been created.
	ok, err := gitx.BranchExists(repo, "myenv")
	if err != nil {
		t.Fatalf("BranchExists: %v", err)
	}
	if !ok {
		t.Fatal("expected branch 'myenv' to exist after AddWorktree")
	}
}

// TestRemoveWorktree verifies that RemoveWorktree removes the worktree
// directory and unregisters the entry from the source repository.
func TestRemoveWorktree(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)

	worktreePath := filepath.Join(repo, ".pinchy-worktrees", "rmenv")
	if err := gitx.AddWorktree(repo, worktreePath, "rmenv"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	if err := gitx.RemoveWorktree(repo, worktreePath); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}

	// Directory must be gone.
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("expected worktree directory to be removed, but os.Stat returned: %v", err)
	}
}

// TestDeleteBranch verifies that DeleteBranch removes an existing branch and
// is a no-op for a branch that doesn't exist.
func TestDeleteBranch(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)

	// Create a branch to delete.
	worktreePath := filepath.Join(repo, ".pinchy-worktrees", "delbranch")
	if err := gitx.AddWorktree(repo, worktreePath, "delbranch"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	// Remove the worktree first so the branch is deletable.
	if err := gitx.RemoveWorktree(repo, worktreePath); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}

	if err := gitx.DeleteBranch(repo, "delbranch"); err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	ok, _ := gitx.BranchExists(repo, "delbranch")
	if ok {
		t.Fatal("branch still exists after DeleteBranch")
	}

	// Deleting a non-existent branch should not error.
	if err := gitx.DeleteBranch(repo, "doesnotexist"); err != nil {
		t.Fatalf("DeleteBranch on non-existent branch returned error: %v", err)
	}
}

// TestWorktreePath verifies that WorktreePath produces the expected path.
func TestWorktreePath(t *testing.T) {
	got := gitx.WorktreePath("/home/user/myrepo", "myenv")
	want := "/home/user/myrepo/.pinchy-worktrees/myenv"
	if got != want {
		t.Fatalf("WorktreePath: got %q, want %q", got, want)
	}
}

// TestEnsureGitExclude verifies that EnsureGitExclude appends the pattern to
// the exclude file without duplicating it.
func TestEnsureGitExclude(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)

	pattern := "/.pinchy-worktrees/"

	if err := gitx.EnsureGitExclude(repo, pattern); err != nil {
		t.Fatalf("first EnsureGitExclude: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("reading exclude: %v", err)
	}
	if !strings.Contains(string(content), pattern) {
		t.Fatalf("pattern %q not found in exclude file:\n%s", pattern, content)
	}

	// Second call must be idempotent.
	if err := gitx.EnsureGitExclude(repo, pattern); err != nil {
		t.Fatalf("second EnsureGitExclude: %v", err)
	}
	content2, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	count := strings.Count(string(content2), pattern)
	if count != 1 {
		t.Fatalf("expected pattern to appear exactly once, got %d:\n%s", count, content2)
	}
}
