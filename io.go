package xcaddy

// credit: https://github.com/goreleaser/goreleaser/blob/3f54b5eb2f13e86f07420124818fb6594f966278/internal/gio/copy.go
import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// copy recursively copies src into dst with src's file modes.
func copy(src, dst string) error {
	src = filepath.ToSlash(src)
	dst = filepath.ToSlash(dst)
	log.Printf("[INFO] copying files: src=%s dest=%s", src, dst)
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("failed to copy %s to %s: %w", src, dst, err)
		}
		path = filepath.ToSlash(path)
		// We have the following:
		// - src = "a/b"
		// - dst = "dist/linuxamd64/b"
		// - path = "a/b/c.txt"
		// So we join "a/b" with "c.txt" and use it as the destination.
		dst := filepath.ToSlash(filepath.Join(dst, strings.Replace(path, src, "", 1)))
		if info.IsDir() {
			return os.MkdirAll(dst, info.Mode())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return copySymlink(path, dst)
		}
		return copyFile(path, dst, info.Mode())
	})
}

func copySymlink(src, dst string) error {
	src, err := os.Readlink(src)
	if err != nil {
		return err
	}
	return os.Symlink(src, dst)
}

func copyFile(src, dst string, mode os.FileMode) error {
	original, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open '%s': %w", src, err)
	}
	defer original.Close()

	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("failed to open '%s': %w", dst, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, original); err != nil {
		return fmt.Errorf("failed to copy: %w", err)
	}
	return nil
}
