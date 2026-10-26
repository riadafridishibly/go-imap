package imapclient

import (
	"fmt"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/internal/imapwire"
)

// Move sends a MOVE command.
//
// If the server doesn't support IMAP4rev2 nor the MOVE extension, Move falls
// back to UID COPY, UID STORE and UID EXPUNGE, each sent after the previous one
// succeeds. The fallback requires UIDPLUS and blocks, so like Wait, don't call
// it while another command's responses are being consumed.
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
		uids, ok = searchData.All.(imap.UIDSet)
		if !ok || len(uids) == 0 {
			return fmt.Errorf("imapclient: no message matches %v in the source mailbox", numSet)
		}
	}

	copyData, err := c.Copy(uids, mailbox).Wait()
	if err != nil {
		return err
	}
	// A dynamic set also matches messages added after the COPY, so only remove
	// what COPYUID reports
	if len(copyData.SourceUIDs) > 0 {
		data.UIDValidity = copyData.UIDValidity
		data.SourceUIDs = copyData.SourceUIDs
		data.DestUIDs = copyData.DestUIDs
		uids = copyData.SourceUIDs
	} else if uids.Dynamic() {
		return fmt.Errorf("imapclient: messages copied, but the server didn't report their UIDs; not removing %v from the source mailbox", uids)
	}

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
	// A fallback that fails after COPY leaves copies at DestUIDs
	return &cmd.data, cmd.wait()
}

// MoveData contains the data returned by a MOVE command.
type MoveData struct {
	// requires UIDPLUS or IMAP4rev2
	UIDValidity uint32
	SourceUIDs  imap.NumSet
	DestUIDs    imap.NumSet
}
