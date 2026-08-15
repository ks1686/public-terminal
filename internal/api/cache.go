package api

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/ks1686/public-terminal/internal/config"
)

// SavePortfolio writes a snapshot to disk. Failures are best-effort and
// returned for the caller to log; nothing depends on the write succeeding.
func SavePortfolio(path string, p *Portfolio) error {
	if p == nil {
		return errors.New("nil portfolio")
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(path, b, 0o600)
}

// LoadPortfolio returns the cached snapshot if present and parseable. A missing
// file is reported as (nil, nil) so callers can treat it as "no cache yet"
// without an os.IsNotExist check.
func LoadPortfolio(path string) (*Portfolio, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var p Portfolio
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
