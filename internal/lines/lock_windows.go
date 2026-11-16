//go:build windows

package lines

import (
	"os"
	"path/filepath"
	"time"
)

func lockAuth(dir string) (func(), error) {
	path := filepath.Join(dir, "codefree.json.lock")
	var f *os.File
	var err error
	for i := 0; i < 50; i++ {
		f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err == nil {
			return func() {
				_ = f.Close()
				_ = os.Remove(path)
			}, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, err
}
