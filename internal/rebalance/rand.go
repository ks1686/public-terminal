package rebalance

import "crypto/rand"

// randRead is a thin wrapper so orders.go can call it without a direct import.
func randRead(b []byte) (int, error) { return rand.Read(b) }
