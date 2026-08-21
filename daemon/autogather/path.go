package autogather

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	DefaultTvLibraryPath = "data/media/tv"
	UserShareRoot        = "/mnt/user"
)

var (
	ErrEmptyTvLibraryPath      = errors.New("tv library path is required")
	ErrAbsoluteTvLibraryPath   = errors.New("tv library path must be relative to /mnt/user, not absolute")
	ErrMisleadingTvLibraryPath = errors.New(`tv library path must not start with "mnt/user"; enter the path relative to /mnt/user`)
	ErrUnsafeTvLibraryPath     = errors.New("tv library path contains an invalid segment")
	ErrOutsideUserShare        = errors.New("tv library path resolves outside /mnt/user")
	ErrUnableToVerifyLibrary   = errors.New("unable to verify tv library path under /mnt/user")
)

// arrayDiskNameRE matches destination-capable Unraid physical array disks only.
// Later-stage Auto Gather target recommendation must consume only disks that
// pass IsArrayDiskName / PartitionDisks array collection — never named pools.
var arrayDiskNameRE = regexp.MustCompile(`^disk[1-9][0-9]*$`)

// IsArrayDiskName reports whether name is a physical Unraid array disk
// (disk1, disk2, …). disk0 and named pools (cache, data_cache, vm_cache,
// appdata, …) never match.
func IsArrayDiskName(name string) bool {
	return arrayDiskNameRE.MatchString(name)
}

// NormalizeTvLibraryPath validates and cleans a path that must remain relative
// to /mnt/user. It does not touch the filesystem.
func NormalizeTvLibraryPath(pathValue string) (string, error) {
	trimmed := strings.TrimSpace(pathValue)
	if trimmed == "" {
		return "", ErrEmptyTvLibraryPath
	}

	if isAbsoluteLibraryPath(trimmed) {
		return "", ErrAbsoluteTvLibraryPath
	}

	slashPath := filepath.ToSlash(trimmed)
	lower := strings.ToLower(slashPath)
	if lower == "mnt/user" || strings.HasPrefix(lower, "mnt/user/") {
		return "", ErrMisleadingTvLibraryPath
	}

	parts := strings.Split(slashPath, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			return "", ErrUnsafeTvLibraryPath
		default:
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return "", ErrEmptyTvLibraryPath
	}

	return path.Join(out...), nil
}

func isAbsoluteLibraryPath(value string) bool {
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, `\`) {
		return true
	}
	if filepath.IsAbs(value) {
		return true
	}
	// Windows-style drive path, rejected for clarity on Unraid too.
	if len(value) >= 2 && value[1] == ':' {
		return true
	}
	return false
}

// EnsureLibraryPathUnderRoot verifies that rel (already normalized) cannot
// resolve outside userRoot through symlinks. Missing leaf paths are allowed
// when every existing parent stays under userRoot.
func EnsureLibraryPathUnderRoot(userRoot, rel string) error {
	if userRoot == "" {
		userRoot = UserShareRoot
	}
	userRoot = filepath.Clean(userRoot)
	target := filepath.Clean(filepath.Join(userRoot, filepath.FromSlash(rel)))
	if !pathIsInside(userRoot, target) {
		return ErrOutsideUserShare
	}

	if _, err := os.Lstat(userRoot); err != nil {
		if os.IsNotExist(err) {
			// Dev / offline hosts without /mnt/user: string validation already passed.
			return nil
		}
		return ErrUnableToVerifyLibrary
	}

	resolvedRoot, err := filepath.EvalSymlinks(userRoot)
	if err != nil {
		return ErrUnableToVerifyLibrary
	}
	resolvedRoot = filepath.Clean(resolvedRoot)

	cur := target
	for {
		fi, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				parent := filepath.Dir(cur)
				if parent == cur || !pathIsInside(userRoot, parent) {
					return nil
				}
				cur = parent
				continue
			}
			return ErrUnableToVerifyLibrary
		}

		resolved := cur
		if fi.Mode()&os.ModeSymlink != 0 || fi.IsDir() {
			if eval, evalErr := filepath.EvalSymlinks(cur); evalErr != nil {
				if os.IsNotExist(evalErr) {
					return nil
				}
				return ErrUnableToVerifyLibrary
			} else {
				resolved = eval
			}
		}

		resolved = filepath.Clean(resolved)
		if !pathIsInside(resolvedRoot, resolved) && resolved != resolvedRoot {
			return ErrOutsideUserShare
		}
		return nil
	}
}

func pathIsInside(root, candidate string) bool {
	root = filepath.Clean(root)
	candidate = filepath.Clean(candidate)
	if candidate == root {
		return true
	}
	prefix := root + string(filepath.Separator)
	return strings.HasPrefix(candidate, prefix)
}

// ValidateTvLibraryPath normalizes pathValue and verifies it under userRoot.
func ValidateTvLibraryPath(pathValue, userRoot string) (string, error) {
	cleaned, err := NormalizeTvLibraryPath(pathValue)
	if err != nil {
		return "", err
	}
	if err := EnsureLibraryPathUnderRoot(userRoot, cleaned); err != nil {
		return "", err
	}
	return cleaned, nil
}

func libraryValidationMessage(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, ErrEmptyTvLibraryPath):
		return "TV library path is required."
	case errors.Is(err, ErrAbsoluteTvLibraryPath):
		return "TV library path must be relative to /mnt/user (do not use an absolute path)."
	case errors.Is(err, ErrMisleadingTvLibraryPath):
		return `Enter the path relative to /mnt/user (for example "data/media/tv"), not "mnt/user/...".`
	case errors.Is(err, ErrUnsafeTvLibraryPath):
		return `TV library path cannot contain ".." segments.`
	case errors.Is(err, ErrOutsideUserShare):
		return "TV library path resolves outside /mnt/user (check for symlinks)."
	case errors.Is(err, ErrUnableToVerifyLibrary):
		return "Unable to verify the TV library path under /mnt/user."
	default:
		return "Invalid TV library path."
	}
}

// FormatLibraryValidationError returns a UI-safe validation message.
func FormatLibraryValidationError(err error) string {
	return libraryValidationMessage(err)
}

// scanWarning builds a browser-safe warning without absolute filesystem paths.
func scanWarning(diskName, relPath, reason string) string {
	diskName = strings.TrimSpace(diskName)
	relPath = strings.Trim(filepath.ToSlash(relPath), "/")
	if diskName == "" {
		diskName = "unknown"
	}
	if relPath == "" {
		return fmt.Sprintf("%s: %s", diskName, reason)
	}
	return fmt.Sprintf("%s: %s (%s)", diskName, relPath, reason)
}

func scanWarningReason(err error) string {
	if err == nil {
		return "skipped"
	}
	if os.IsPermission(err) {
		return "permission denied"
	}
	return "inaccessible"
}
