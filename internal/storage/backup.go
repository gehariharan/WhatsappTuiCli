package storage

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/gehariharan/wasync/internal/localfs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// keepBackups is how many pre-migration backups are retained per archive.
const keepBackups = 3

// backupNow is the backup timestamp clock; tests replace it.
var backupNow = time.Now

// backupArchive writes a consistent copy of the open archive to
// <archive>.pre-v<version>-<UTC yyyymmddThhmmssZ>.bak before migrations run.
//
// VACUUM INTO reads one snapshot through the live connection, so committed WAL
// frames are included without a checkpoint. The target is created first as an
// empty private file (0600 on Unix, owner-only DACL on Windows) so the copy is
// never readable by other users, and it is fsynced with its directory before
// any migration starts. Disk use: each backup is a compacted full copy of the
// archive (media files are not part of it), so upgrades need free space for
// one more copy and up to keepBackups copies stay next to the archive. Older
// backups matching the exact naming pattern are pruned best-effort.
func backupArchive(ctx context.Context, db *sql.DB, path string, version int) (string, error) {
	name := fmt.Sprintf("%s.pre-v%d-%s.bak", path, version, backupNow().UTC().Format("20060102T150405Z"))
	if _, err := os.Lstat(name); err == nil {
		return "", fmt.Errorf("backup %s already exists", name)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	f, err := localfs.OpenFile(name)
	if err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	if _, err = db.ExecContext(ctx, "VACUUM INTO ?", name); err != nil {
		os.Remove(name)
		return "", err
	}
	if err = syncFile(name); err != nil {
		os.Remove(name)
		return "", err
	}
	if err = localfs.SyncDir(filepath.Dir(name)); err != nil {
		return "", err
	}
	pruneBackups(path, keepBackups)
	return name, nil
}

func syncFile(name string) error {
	f, err := localfs.OpenFile(name)
	if err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// pruneBackups deletes all but the newest keep backups of path. Only regular
// files whose names match the exact backup pattern are considered.
func pruneBackups(path string, keep int) {
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	pattern := regexp.MustCompile(`^` + regexp.QuoteMeta(base) + `\.pre-v([0-9]+)-([0-9]{8}T[0-9]{6}Z)\.bak$`)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type backup struct{ name, stamp, version string }
	var backups []backup
	for _, e := range entries {
		m := pattern.FindStringSubmatch(e.Name())
		if m == nil || !e.Type().IsRegular() {
			continue
		}
		backups = append(backups, backup{e.Name(), m[2], m[1]})
	}
	sort.Slice(backups, func(i, j int) bool {
		if backups[i].stamp != backups[j].stamp {
			return backups[i].stamp > backups[j].stamp
		}
		if len(backups[i].version) != len(backups[j].version) {
			return len(backups[i].version) > len(backups[j].version)
		}
		return backups[i].version > backups[j].version
	})
	for i := keep; i < len(backups); i++ {
		os.Remove(filepath.Join(dir, backups[i].name))
	}
}
