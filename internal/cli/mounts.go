package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/mount"

	"github.com/nickschuch/pinchy/internal/config"
)

// resolvedMount bundles a Docker mount.Mount with the original (unexpanded)
// source string so the caller can print a human-readable summary.
type resolvedMount struct {
	mount       mount.Mount
	origSource  string
}

// resolveMounts converts a slice of config.Mount entries into Docker SDK
// mount.Mount values ready to pass to AgentSpec.ExtraMounts.  It also returns
// a display-friendly summary line for each mount.
//
// For each entry:
//   - A leading "~/" or a bare "~" in Source is expanded to os.UserHomeDir().
//   - filepath.Abs is applied so relative paths become absolute.
//   - Symlinks are NOT resolved (Docker handles them on the daemon side).
//   - The source must exist and must be a directory; a non-existent source or a
//     plain file is a hard error.
//   - Mode defaults to "ro" when empty; "rw" produces a read-write mount.
func resolveMounts(mounts []config.Mount) ([]resolvedMount, error) {
	if len(mounts) == 0 {
		return nil, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolving home directory: %w", err)
	}

	out := make([]resolvedMount, 0, len(mounts))
	for _, m := range mounts {
		abs, err := expandAndAbs(m.Source, home)
		if err != nil {
			return nil, fmt.Errorf("mount source %q: %w", m.Source, err)
		}

		fi, err := os.Stat(abs)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("mount source %q (%s) does not exist", m.Source, abs)
			}
			return nil, fmt.Errorf("mount source %q: %w", m.Source, err)
		}
		if !fi.IsDir() {
			return nil, fmt.Errorf("mount source %q (%s) is not a directory", m.Source, abs)
		}

		readOnly := true
		if m.Mode == "rw" {
			readOnly = false
		}

		out = append(out, resolvedMount{
			mount: mount.Mount{
				Type:     mount.TypeBind,
				Source:   abs,
				Target:   m.Target,
				ReadOnly: readOnly,
			},
			origSource: m.Source,
		})
	}
	return out, nil
}

// expandAndAbs expands a leading "~" or "~/" to home, then calls filepath.Abs.
func expandAndAbs(src, home string) (string, error) {
	if src == "~" {
		src = home
	} else if strings.HasPrefix(src, "~/") {
		src = filepath.Join(home, src[2:])
	}
	return filepath.Abs(src)
}
