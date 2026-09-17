package imapclient

import (
	"fmt"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/internal/imapwire"
)

// Move sends a MOVE command.
//
// If the server doesn't support IMAP4rev2 nor the MOVE extension, Move falls
// back to UID COPY, UID STORE +FLAGS.SILENT \Deleted and UID EXPUNGE, and
// blocks until they complete. Each command is sent after the previous one
// succeeds, so messages are only removed from the source mailbox once they
// have been copied. The fallback requires the UIDPLUS extension: without it,
// an error is returned and no command is sent. A SeqSet is first converted to
// UIDs with UID SEARCH.
func (c *Client) Move(numSet imap.NumSet, mailbox string) *MoveCommand {
	cmd := &MoveCommand{}
	if !c.Caps().Has(imap.CapMove) {
		cmd.done = make(chan error, 1)
		cmd.done <- c.moveFallback(numSet, mailbox, &cmd.data)
		return cmd
	}

	enc := c.beginCommand(uidCmdName("MOVE", imapwire.NumSetKind(numSet)), cmd)
	enc.SP().NumSet(numSet).SP().Mailbox(mailbox)
	enc.end()
	return cmd
}

func (c *Client) moveFallback(numSet imap.NumSet, mailbox string, data *MoveData) error {
	// Plain EXPUNGE would also remove messages flagged \Deleted by others
	if !c.Caps().Has(imap.CapUIDPlus) {
		return fmt.Errorf("imapclient: server has advertised neither MOVE nor UIDPLUS")
	}

	uids, ok := numSet.(imap.UIDSet)
	if !ok {
		// Sequence numbers can shift under an EXPUNGE sent during COPY or STORE
		searchData, err := c.UIDSearch(&imap.SearchCriteria{
			SeqNum: []imap.SeqSet{numSet.(imap.SeqSet)},
		}, nil).Wait()
		if err != nil {
			return err
		}
		uids, _ = searchData.All.(imap.UIDSet)
		if len(uids) == 0 {
			return nil
		}
	}

	copyData, err := c.Copy(uids, mailbox).Wait()
	if err != nil {
		return err
	}
	data.UIDValidity = copyData.UIDValidity
	data.SourceUIDs = copyData.SourceUIDs
	data.DestUIDs = copyData.DestUIDs

	err = c.Store(uids, &imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Silent: true,
		Flags:  []imap.Flag{imap.FlagDeleted},
	}, nil).Close()
	if err != nil {
		return fmt.Errorf("imapclient: messages copied, but not flagged \\Deleted in the source mailbox: %w", err)
	}

	if err := c.UIDExpunge(uids).Close(); err != nil {
		return fmt.Errorf("imapclient: messages copied and flagged \\Deleted, but not expunged from the source mailbox: %w", err)
	}
	return nil
}

// MoveCommand is a MOVE command.
type MoveCommand struct {
	commandBase
	data MoveData
}

func (cmd *MoveCommand) Wait() (*MoveData, error) {
	if err := cmd.wait(); err != nil {
		return nil, err
	}
	return &cmd.data, nil
}

// MoveData contains the data returned by a MOVE command.
type MoveData struct {
	// requires UIDPLUS or IMAP4rev2
	UIDValidity uint32
	SourceUIDs  imap.NumSet
	DestUIDs    imap.NumSet
}
