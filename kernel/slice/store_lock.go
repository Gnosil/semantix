package slice

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrStoreBusy means a live store handle prevents an exclusive maintenance
// snapshot. Readers/writers keep a shared lease until Close; maintenance takes
// an exclusive lease. The stable sidecar must never be removed after unlock.
var ErrStoreBusy = errors.New("slice: store is in use; close gateway and other store handles before maintenance")

func canonicalStorePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(abs)), nil
}

func lockStore(path string, exclusive, create bool) (*os.File, error) {
	flags := os.O_RDONLY
	if create {
		flags = os.O_CREATE | os.O_RDWR
	}
	f, err := os.OpenFile(path+".maintenance.lock", flags, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockStoreFile(f, exclusive); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
