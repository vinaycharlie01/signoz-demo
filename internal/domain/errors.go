package domain

// ErrInvalidOrder reports a violated Order invariant. It is a string type
// (not wrapped stdlib errors) so adapters can type-switch on it without
// importing a shared sentinel-error package.
type ErrInvalidOrder string

func (e ErrInvalidOrder) Error() string { return "invalid order: " + string(e) }

// ErrNotFound reports that an Order does not exist.
type ErrNotFound struct {
	ID string
}

func (e ErrNotFound) Error() string { return "order not found: " + e.ID }
