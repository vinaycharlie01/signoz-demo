// Package idgen is a tiny driven adapter implementing ports.IDGenerator.
package idgen

import "github.com/google/uuid"

// UUID generates random (v4) UUIDs.
type UUID struct{}

// NewID returns a new random UUID string.
func (UUID) NewID() string { return uuid.NewString() }
