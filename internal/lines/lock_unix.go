//go:build unix

package lines

import (
	"os"
	"path/filepath"
	"syscall"
)

func lockAuth(dir string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, "codefree.json.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
