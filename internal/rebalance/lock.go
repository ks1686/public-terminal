package rebalance

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"

	"github.com/ks1686/public-terminal/internal/config"
)

func acquireAccountLock(accountID string) (func(), error) {
	path := config.RebalanceLockPath(accountID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening rebalance lock: %w", err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("rebalance already running for this account")
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		f.Close()
	}, nil
}
