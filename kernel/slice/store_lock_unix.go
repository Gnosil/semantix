//go:build !windows

package slice

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func lockStoreFile(f *os.File, exclusive bool) error {
	mode := unix.LOCK_SH | unix.LOCK_NB
	if exclusive {
		mode = unix.LOCK_EX | unix.LOCK_NB
	}
	err := unix.Flock(int(f.Fd()), mode)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return ErrStoreBusy
	}
	return err
}
