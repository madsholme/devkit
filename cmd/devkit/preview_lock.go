package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func lockPreview(name string) (func(), error) {
	if !previewID.MatchString(name) {
		return nil, fail("Use an exact preview ID")
	}
	dir := filepath.Join(stateDir(), "preview-locks")
	if err := privateDir(dir); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(filepath.Join(dir, name+".lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	if err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fail("Another lifecycle operation holds this preview; retry after it finishes.")
	}
	return func() { syscall.Flock(fd, syscall.LOCK_UN); f.Close() }, nil
}
func rebuildDir(name string) string { return filepath.Join(stateDir(), "rebuilds", name) }
func loadRebuildOperation(name string) (*rebuildOperation, error) {
	if !previewID.MatchString(name) {
		return nil, fail("Invalid preview ID")
	}
	b, err := readPrivate(filepath.Join(rebuildDir(name), "operation.json"), 16<<20)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var op rebuildOperation
	if json.Unmarshal(b, &op) != nil || op.Preview != name || op.Phase == "" {
		return nil, fail("Invalid rebuild recovery record")
	}
	return &op, nil
}
func saveRebuildOperation(op rebuildOperation) error {
	if !previewID.MatchString(op.Preview) {
		return fail("Invalid preview ID")
	}
	if err := privateDir(filepath.Join(stateDir(), "rebuilds")); err != nil {
		return err
	}
	if err := privateDir(rebuildDir(op.Preview)); err != nil {
		return err
	}
	b, err := json.Marshal(op)
	if err != nil {
		return err
	}
	if len(b) > 16<<20 {
		return fail("Rebuild record exceeds limit")
	}
	return writePrivate(filepath.Join(rebuildDir(op.Preview), "operation.json"), b)
}
func previewRecoveryGuard(name, action string) error {
	op, err := loadRebuildOperation(name)
	if err != nil {
		return fail("Rebuild recovery state is unreadable; inspect it before changing this preview.")
	}
	if op != nil && op.Phase != "complete" && action != "stop" {
		return fail("An unfinished rebuild blocks this action; inspect devkit rebuild %s --recover first.", name)
	}
	return nil
}
func recordRecoveryStop(name string) error {
	op, err := loadRebuildOperation(name)
	if err != nil {
		return err
	}
	if op != nil && op.Phase != "complete" {
		rows, err := rebuildInventory(name)
		if err != nil {
			return fail("Stop completed but runtime observation is unavailable; recovery remains pending.")
		}
		running, err := validateRebuildRuntime(*op, rows)
		if err != nil || running {
			return fail("Cannot verify this recovery target is stopped; recovery remains pending.")
		}
		op.Stopped = true
		op.Runtime = []previewContainerView{}
		for _, c := range rows {
			op.Runtime = append(op.Runtime, projectContainerView(c))
		}
		return saveRebuildOperation(*op)
	}
	return nil
}
