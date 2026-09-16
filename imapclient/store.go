package imapclient

import (
	"fmt"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/internal/imapwire"
	"github.com/emersion/go-imap/v2/internal/utf7"
)

// Store sends a STORE command.
//
// Unless StoreFlags.Silent is set, the server will return the updated values.
//
// A nil options pointer is equivalent to a zero options value.
func (c *Client) Store(numSet imap.NumSet, store *imap.StoreFlags, options *imap.StoreOptions) *FetchCommand {
	cmd, enc := c.beginStore(numSet, store.Op, "FLAGS", store.Silent, options)
	enc.List(len(store.Flags), func(i int) {
		enc.Flag(store.Flags[i])
	})
	enc.end()
	return cmd
}

// StoreGmailLabels sends a STORE command for X-GM-LABELS.
//
// Unless StoreGmailLabels.Silent is set, the server will return the updated
// values.
//
// Gmail behavior to be aware of:
//
//   - A label that does not exist is created.
//   - Removing the label of the selected mailbox succeeds but changes nothing.
//     Remove it from the mailbox with the \All attribute instead.
//   - Adding or removing \Starred also changes \Flagged, and Gmail sends a
//     FETCH response with FLAGS for it, even with Silent set. Without Silent
//     it is a second response for the same message, which goes to the
//     unilateral FETCH handler, not to the returned command.
//
// A nil options pointer is equivalent to a zero options value.
//
// This requires the X-GM-EXT-1 extension.
func (c *Client) StoreGmailLabels(numSet imap.NumSet, store *imap.StoreGmailLabels, options *imap.StoreOptions) *FetchCommand {
	cmd, enc := c.beginStore(numSet, store.Op, "X-GM-LABELS", store.Silent, options)
	enc.List(len(store.Labels), func(i int) {
		// Gmail takes modified UTF-7 names outside UTF-8 mode
		label := store.Labels[i]
		if !enc.QuotedUTF8 {
			label = utf7.Encode(label)
		}
		enc.String(label)
	})
	enc.end()
	return cmd
}

// beginStore writes a STORE command up to the value list.
func (c *Client) beginStore(numSet imap.NumSet, op imap.StoreFlagsOp, item string, silent bool, options *imap.StoreOptions) (*FetchCommand, *commandEncoder) {
	cmd := &FetchCommand{
		numSet: numSet,
		msgs:   make(chan *FetchMessageData, 128),
	}
	enc := c.beginCommand(uidCmdName("STORE", imapwire.NumSetKind(numSet)), cmd)
	enc.SP().NumSet(numSet).SP()
	if options != nil && options.UnchangedSince != 0 {
		enc.Special('(').Atom("UNCHANGEDSINCE").SP().ModSeq(options.UnchangedSince).Special(')').SP()
	}
	switch op {
	case imap.StoreFlagsSet:
		// nothing to do
	case imap.StoreFlagsAdd:
		enc.Special('+')
	case imap.StoreFlagsDel:
		enc.Special('-')
	default:
		panic(fmt.Errorf("imapclient: unknown store flags op: %v", op))
	}
	enc.Atom(item)
	if silent {
		enc.Atom(".SILENT")
	}
	enc.SP()
	return cmd, enc
}
