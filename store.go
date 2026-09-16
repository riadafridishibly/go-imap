package imap

// StoreOptions contains options for the STORE command.
type StoreOptions struct {
	UnchangedSince uint64 // requires CONDSTORE
}

// StoreFlagsOp is a flag operation: set, add or delete.
type StoreFlagsOp int

const (
	StoreFlagsSet StoreFlagsOp = iota
	StoreFlagsAdd
	StoreFlagsDel
)

// StoreFlags alters message flags.
type StoreFlags struct {
	Op     StoreFlagsOp
	Silent bool
	Flags  []Flag
}

// StoreGmailLabels alters Gmail labels.
//
// This requires the X-GM-EXT-1 extension.
type StoreGmailLabels struct {
	Op     StoreFlagsOp
	Silent bool
	Labels []string
}
