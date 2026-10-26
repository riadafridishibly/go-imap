package imapclient

import (
	"fmt"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/internal/imapwire"
)

// Move sends a MOVE command.
//
// If the server doesn't support IMAP4rev2 nor the MOVE extension, Move falls
// back to COPY, UID STORE and UID EXPUNGE, each sent after the previous one
// succeeds. Only the messages listed in COPYUID are removed. The fallback
// requires UIDPLUS and blocks, so like Wait, don't call it while another
// command's responses are being consumed.
func (c *Client) Move(numSet imap.NumSet, mailbox string) *MoveCommand {
	cmd := &MoveCommand{}
	caps, err := c.waitCaps()
	if err == nil && caps.Has(imap.CapMove) {
		enc := c.beginCommand(uidCmdName("MOVE", imapwire.NumSetKind(numSet)), cmd)
		enc.SP().NumSet(numSet).SP().Mailbox(mailbox)
		enc.end()
		return cmd
	}

	if err != nil {
		err = fmt.Errorf("imapclient: capabilities unknown, not moving: %w", err)
	} else {
		err = c.moveFallback(caps, numSet, mailbox, &cmd.data)
	}
	cmd.done = make(chan error, 1)
	cmd.done <- err
	close(cmd.done) // as completeCommand does, so that Wait can be called again
	return cmd
}

func (c *Client) moveFallback(caps imap.CapSet, numSet imap.NumSet, mailbox string, data *MoveData) error {
	// Plain EXPUNGE would also remove messages flagged \Deleted by others
	if !caps.Has(imap.CapUIDPlus) {
		return fmt.Errorf("imapclient: server has advertised neither MOVE nor UIDPLUS")
	}

	var selects uint64
	copyCmd, err := c.copyIf(numSet, mailbox, func() error {
		// COPY would succeed, then STORE would fail. c.mailbox is stale while
		// a SELECT is in flight.
		if c.selectsDone == c.selectsSent && c.mailbox != nil && c.mailbox.ReadOnly {
			return fmt.Errorf("imapclient: mailbox %q is read-only", c.mailbox.Name)
		}
		selects = c.selectsSent
		return nil
	})
	if err != nil {
		return err
	}
	copyData, err := copyCmd.Wait()
	if err != nil {
		return err
	}
	// COPYUID lists what was copied, even if sequence numbers shifted or
	// messages were added to a range after COPY
	if len(copyData.SourceUIDs) == 0 {
		return fmt.Errorf("imapclient: COPY succeeded without COPYUID; not removing messages from the source mailbox")
	}
	data.UIDValidity = copyData.UIDValidity
	data.SourceUIDs = copyData.SourceUIDs
	data.DestUIDs = copyData.DestUIDs
	uids := copyData.SourceUIDs

	// STORE and EXPUNGE must run in the mailbox COPY ran in
	sameMailbox := func() error {
		if c.selectsSent != selects {
			return fmt.Errorf("imapclient: another mailbox was selected after COPY")
		}
		return nil
	}

	storeCmd, err := c.storeIf(uids, &imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Silent: true,
		Flags:  []imap.Flag{imap.FlagDeleted},
	}, nil, sameMailbox)
	if err == nil {
		err = storeCmd.Close()
	}
	if err != nil {
		return fmt.Errorf("imapclient: messages copied, but not flagged \\Deleted in the source mailbox: %w", err)
	}

	// Not an ExpungeCommand, so the expunges reach UnilateralDataHandler.Expunge
	// as with MOVE
	expungeCmd := &Command{}
	enc, err := c.beginCommandIf("UID EXPUNGE", expungeCmd, sameMailbox)
	if err == nil {
		enc.SP().NumSet(uids)
		enc.end()
		err = expungeCmd.Wait()
	}
	if err != nil {
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
