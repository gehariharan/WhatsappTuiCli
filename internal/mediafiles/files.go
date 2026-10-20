// Package mediafiles names local downloads using opaque hashes, never sender filenames.
package mediafiles

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gehariharan/wasync/internal/localfs"
)

func Directory(root string, account int64, chat string) string {
	digest := sha256.Sum256([]byte(chat))
	return filepath.Join(root, strconv.FormatInt(account, 10), fmt.Sprintf("%x", digest))
}
func Prefix(message string) string {
	digest := sha256.Sum256([]byte(message))
	return fmt.Sprintf("%x", digest)
}
func Ensure(root string, account int64, chat string) (string, error) {
	for _, dir := range []string{root, filepath.Join(root, strconv.FormatInt(account, 10)), Directory(root, account, chat)} {
		if err := localfs.EnsureDir(dir); err != nil {
			return "", err
		}
	}
	return Directory(root, account, chat), nil
}
func RemoveMessage(root string, account int64, chat, message string) error {
	if root == "" {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(Directory(root, account, chat), Prefix(message)+"-*"))
	if err != nil {
		return err
	}
	for _, path := range matches {
		if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

var managedName = regexp.MustCompile(`^[a-f0-9]{64}-[a-f0-9]{64}\.(bin|jpg|png|webp|gif|pdf|mp4|3gp|webm|ogg|mp3|m4a|aac|wav|doc|docx|xls|xlsx|ppt|pptx|txt|csv|zip)(\.part)?$`)
var chatDirectory = regexp.MustCompile(`^[a-f0-9]{64}$`)

func ManagedPath(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) != 3 {
		return false
	}
	account, err := strconv.ParseInt(parts[0], 10, 64)
	return err == nil && account > 0 && chatDirectory.MatchString(parts[1]) && managedName.MatchString(parts[2])
}
