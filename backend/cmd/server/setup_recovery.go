package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const installAdvisoryLock int64 = 0x4450494E5354414C

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type installJournalPhase string

const (
	installPhasePrepared       installJournalPhase = "prepared"
	installPhaseAdminPending   installJournalPhase = "admin_pending"
	installPhaseAdminReverted  installJournalPhase = "admin_reverted"
	installPhaseConfigReverted installJournalPhase = "config_reverted"
)

type installJournal struct {
	InstallID         string              `json:"install_id"`
	Phase             installJournalPhase `json:"phase"`
	AdminID           string              `json:"admin_id"`
	PriorConfigExists bool                `json:"prior_config_exists"`
	BackupPath        string              `json:"backup_path"`
	BackupSHA256      string              `json:"backup_sha256"`
}

type processFileLock struct {
	file *os.File
}

type databaseInstallLock struct {
	conn *sql.Conn
}

func installFileLockPath() string {
	return installLockPath() + ".lock"
}

func installJournalPath() string {
	return installLockPath() + ".recovery.json"
}

func installRecoveryExists() bool {
	_, err := os.Stat(installJournalPath())
	return err == nil
}

func acquireProcessFileLock(ctx context.Context) (*processFileLock, error) {
	file, err := os.OpenFile(installFileLockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, err
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &processFileLock{file: file}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = file.Close()
			return nil, err
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (lock *processFileLock) verify() error {
	if lock == nil || lock.file == nil {
		return errors.New("install file lock is not held")
	}
	if err := syscall.Flock(int(lock.file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("verify install file lock: %w", err)
	}
	return nil
}

func (lock *processFileLock) release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	err := syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN)
	closeErr := lock.file.Close()
	lock.file = nil
	return errors.Join(err, closeErr)
}

func acquireDatabaseInstallLock(ctx context.Context, db *sql.DB) (*databaseInstallLock, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	for {
		var acquired bool
		if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, installAdvisoryLock).Scan(&acquired); err != nil {
			_ = conn.Close()
			return nil, err
		}
		if acquired {
			return &databaseInstallLock{conn: conn}, nil
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = conn.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (lock *databaseInstallLock) verify(ctx context.Context) error {
	if lock == nil || lock.conn == nil {
		return errors.New("database install lock is not held")
	}
	var held bool
	if err := lock.conn.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_locks
			WHERE locktype = 'advisory'
			  AND pid = pg_backend_pid()
			  AND mode = 'ExclusiveLock'
			  AND granted
			  AND classid = (($1::bigint >> 32) & 4294967295)::oid
			  AND objid = ($1::bigint & 4294967295)::oid
			  AND objsubid = 1
		)
	`, installAdvisoryLock).Scan(&held); err != nil {
		return err
	}
	if !held {
		return errors.New("database install lock ownership was lost")
	}
	return nil
}

func (lock *databaseInstallLock) release(ctx context.Context) error {
	if lock == nil || lock.conn == nil {
		return nil
	}
	var released bool
	err := lock.conn.QueryRowContext(ctx, `SELECT pg_advisory_unlock($1)`, installAdvisoryLock).Scan(&released)
	if err == nil && !released {
		err = errors.New("database install lock was not held")
	}
	if err != nil {
		// A failed unlock must not return a possibly lock-owning session to the pool.
		err = errors.Join(err, lock.conn.Raw(func(any) error { return driver.ErrBadConn }))
	}
	closeErr := lock.conn.Close()
	lock.conn = nil
	return errors.Join(err, closeErr)
}

func prepareInstallJournal(snapshot fileSnapshot, installID, adminID string) (installJournal, error) {
	backupPath := snapshot.path + ".backup-" + installID
	backupData := snapshot.data
	if !snapshot.exists {
		backupData = []byte{}
	}
	if err := writeFileDurable(backupPath, backupData, 0o600, installID); err != nil {
		return installJournal{}, err
	}
	digest := sha256.Sum256(backupData)
	journal := installJournal{
		InstallID:         installID,
		Phase:             installPhasePrepared,
		AdminID:           adminID,
		PriorConfigExists: snapshot.exists,
		BackupPath:        backupPath,
		BackupSHA256:      hex.EncodeToString(digest[:]),
	}
	if err := writeInstallJournal(journal); err != nil {
		_ = removeDurable(backupPath)
		return installJournal{}, err
	}
	return journal, nil
}

func writeInstallJournal(journal installJournal) error {
	if err := validateInstallJournal(journal); err != nil {
		return err
	}
	data, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileDurable(installJournalPath(), data, 0o600, journal.InstallID)
}

func readInstallJournal() (installJournal, error) {
	data, err := os.ReadFile(installJournalPath())
	if err != nil {
		return installJournal{}, err
	}
	var journal installJournal
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&journal); err != nil {
		return installJournal{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return installJournal{}, errors.New("recovery journal contains trailing data")
	}
	if err := validateInstallJournal(journal); err != nil {
		return installJournal{}, err
	}
	return journal, nil
}

func validateInstallJournal(journal installJournal) error {
	if !uuidPattern.MatchString(journal.InstallID) || !uuidPattern.MatchString(journal.AdminID) {
		return errors.New("invalid recovery journal identifier")
	}
	switch journal.Phase {
	case installPhasePrepared, installPhaseAdminPending, installPhaseAdminReverted, installPhaseConfigReverted:
	default:
		return errors.New("invalid recovery journal phase")
	}
	expectedBackup := runtimeEnvPath() + ".backup-" + journal.InstallID
	if filepath.Clean(journal.BackupPath) != filepath.Clean(expectedBackup) {
		return errors.New("invalid recovery journal backup path")
	}
	if len(journal.BackupSHA256) != sha256.Size*2 {
		return errors.New("invalid recovery journal backup digest")
	}
	if _, err := hex.DecodeString(journal.BackupSHA256); err != nil {
		return errors.New("invalid recovery journal backup digest")
	}
	return nil
}

func advanceInstallJournal(journal *installJournal, phase installJournalPhase) error {
	journal.Phase = phase
	return writeInstallJournal(*journal)
}

func recoverInstallation(ctx context.Context) (resultErr error) {
	installMu.Lock()
	defer installMu.Unlock()
	lock, err := acquireProcessFileLock(ctx)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, lock.release())
	}()
	return recoverInstallationLocked(ctx)
}

func recoverInstallationLocked(ctx context.Context) error {
	journal, err := readInstallJournal()
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read recovery journal: %w", err)
	}
	if installLockExists() {
		return cleanupInstallArtifacts(journal)
	}
	if journal.Phase == installPhaseAdminPending {
		databaseURL, err := databaseURLFromRuntimeConfig()
		if err != nil {
			return fmt.Errorf("read recovery database target: %w", err)
		}
		db, err := sql.Open("postgres", databaseURL)
		if err != nil {
			return fmt.Errorf("open recovery database: %w", err)
		}
		lock, err := acquireDatabaseInstallLock(ctx, db)
		if err != nil {
			_ = db.Close()
			return fmt.Errorf("acquire recovery database lock: %w", err)
		}
		_, deleteErr := db.ExecContext(ctx, `DELETE FROM users WHERE id = $1 AND is_admin = true`, journal.AdminID)
		if deleteErr == nil {
			deleteErr = advanceInstallJournal(&journal, installPhaseAdminReverted)
		}
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		releaseErr := lock.release(releaseCtx)
		cancel()
		closeErr := db.Close()
		if err := errors.Join(deleteErr, releaseErr, closeErr); err != nil {
			return fmt.Errorf("revert recovery administrator: %w", err)
		}
	}
	if journal.Phase == installPhasePrepared || journal.Phase == installPhaseAdminReverted {
		if err := restoreRuntimeConfig(journal); err != nil {
			return fmt.Errorf("restore runtime configuration: %w", err)
		}
		if err := advanceInstallJournal(&journal, installPhaseConfigReverted); err != nil {
			return fmt.Errorf("record restored runtime configuration: %w", err)
		}
	}
	if journal.Phase == installPhaseConfigReverted {
		return cleanupInstallArtifacts(journal)
	}
	return nil
}

func rollbackInstallBeforeMarker(ctx context.Context, db *sql.DB, journal *installJournal) error {
	if journal.Phase == installPhaseAdminPending {
		if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = $1 AND is_admin = true`, journal.AdminID); err != nil {
			return err
		}
		if err := advanceInstallJournal(journal, installPhaseAdminReverted); err != nil {
			return err
		}
	}
	if journal.Phase == installPhasePrepared || journal.Phase == installPhaseAdminReverted {
		if err := restoreRuntimeConfig(*journal); err != nil {
			return err
		}
		if err := advanceInstallJournal(journal, installPhaseConfigReverted); err != nil {
			return err
		}
	}
	return cleanupInstallArtifacts(*journal)
}

func restoreRuntimeConfig(journal installJournal) error {
	backup, err := os.ReadFile(journal.BackupPath)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(backup)
	if hex.EncodeToString(digest[:]) != journal.BackupSHA256 {
		return errors.New("runtime configuration backup digest mismatch")
	}
	if journal.PriorConfigExists {
		return writeFileDurable(runtimeEnvPath(), backup, 0o600, journal.InstallID)
	}
	return removeDurable(runtimeEnvPath())
}

func cleanupInstallArtifacts(journal installJournal) error {
	var result error
	for _, path := range installTemporaryPaths(journal) {
		if err := removeDurable(path); err != nil {
			result = errors.Join(result, err)
		}
	}
	if result != nil {
		return result
	}
	if err := removeDurable(journal.BackupPath); err != nil {
		return err
	}
	return removeDurable(installJournalPath())
}

func installTemporaryPaths(journal installJournal) []string {
	patterns := []string{
		filepath.Join(filepath.Dir(runtimeEnvPath()), "."+filepath.Base(runtimeEnvPath())+"."+journal.InstallID+".tmp-*"),
		filepath.Join(filepath.Dir(installLockPath()), "."+filepath.Base(installLockPath())+"."+journal.InstallID+".tmp-*"),
		filepath.Join(filepath.Dir(installJournalPath()), "."+filepath.Base(installJournalPath())+"."+journal.InstallID+".tmp-*"),
		filepath.Join(filepath.Dir(journal.BackupPath), "."+filepath.Base(journal.BackupPath)+"."+journal.InstallID+".tmp-*"),
	}
	var paths []string
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		paths = append(paths, matches...)
	}
	return paths
}

func writeFileDurable(path string, data []byte, mode os.FileMode, installID string) error {
	dir := filepath.Dir(path)
	prefix := "." + filepath.Base(path) + "." + installID + ".tmp-"
	file, err := os.CreateTemp(dir, prefix)
	if err != nil {
		return err
	}
	tempPath := file.Name()
	removeTemp := true
	defer func() {
		_ = file.Close()
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	removeTemp = false
	return syncDirectory(dir)
}

func removeDurable(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func databaseURLFromRuntimeConfig() (string, error) {
	values, err := readEnvValues(runtimeEnvPath())
	if err != nil {
		return "", err
	}
	databaseURL := strings.TrimSpace(values["DATABASE_URL"])
	if err := validateDatabaseURL(databaseURL); err != nil {
		return "", errors.New("recovery database configuration is invalid")
	}
	return databaseURL, nil
}
