package imapclient

import (
	"errors"
	"fmt"
	"slices"

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
		err = c.moveFallback(caps, numSet, mailbox, cmd)
	}
	cmd.done = make(chan error, 1)
	cmd.done <- err
	close(cmd.done) // as completeCommand does, so that Wait can be called again
	return cmd
}

func (c *Client) moveFallback(caps imap.CapSet, numSet imap.NumSet, mailbox string, cmd *MoveCommand) error {
	// Plain EXPUNGE would also remove messages flagged \Deleted by others
	if !caps.Has(imap.CapUIDPlus) {
		return fmt.Errorf("imapclient: server has advertised neither MOVE nor UIDPLUS")
	}

	// c.mailbox is stale until every SELECT, CLOSE, etc. sent so far is done
	c.mutex.Lock()
	for c.mboxCmdsDone != c.mboxCmdsSent {
		c.mboxCmdsCond.Wait()
	}
	c.mutex.Unlock()

	var mboxCmds uint64
	copyCmd, err := c.copyIf(numSet, mailbox, func() error {
		if c.mboxCmdsDone != c.mboxCmdsSent {
			return fmt.Errorf("imapclient: the selected mailbox changed during Move")
		}
		// COPY would succeed, then STORE would fail or be ignored
		if mbox := c.mailbox; mbox != nil {
			if mbox.ReadOnly {
				return fmt.Errorf("imapclient: mailbox %q is read-only", mbox.Name)
			}
			// Some servers list only \*
			if flags := mbox.PermanentFlags; flags != nil && !slices.Contains(flags, imap.FlagDeleted) && !slices.Contains(flags, imap.FlagWildcard) {
				return fmt.Errorf("imapclient: mailbox %q doesn't allow \\Deleted", mbox.Name)
			}
		}
		mboxCmds = c.mboxCmdsSent
		return nil
	})
	if err != nil {
		return err
	}
	copyData, err := copyCmd.Wait()
	if err != nil {
		return err
	}
	cmd.copied = true

	// The other commands must run in the mailbox COPY ran in
	sameMailbox := func() error {
		if c.mboxCmdsSent != mboxCmds {
			return fmt.Errorf("imapclient: the selected mailbox changed after COPY")
		}
		return nil
	}

	// COPYUID lists what was copied, even if sequence numbers shifted or
	// messages were added to a range after COPY
	if len(copyData.SourceUIDs) == 0 {
		// COPYUID can't be empty, so servers leave it out if nothing matched.
		// Copied messages are still in the source mailbox.
		found, err := c.anyMessages(numSet, sameMailbox)
		if err == nil && !found {
			return nil
		}
		msg := "imapclient: COPY succeeded without COPYUID; not removing messages from the source mailbox"
		if err != nil {
			return fmt.Errorf("%s: %w", msg, err)
		}
		return errors.New(msg)
	}
	cmd.data.UIDValidity = copyData.UIDValidity
	cmd.data.SourceUIDs = copyData.SourceUIDs
	cmd.data.DestUIDs = copyData.DestUIDs
	uids := copyData.SourceUIDs

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
	if err != nil {
		// A CLOSE may have expunged them
		return fmt.Errorf("imapclient: messages copied and flagged \\Deleted; UID EXPUNGE not sent: %w", err)
	}
	enc.SP().NumSet(uids)
	enc.end()
	if err := expungeCmd.Wait(); err != nil {
		return fmt.Errorf("imapclient: messages copied and flagged \\Deleted, but not expunged from the source mailbox: %w", err)
	}
	return nil
}

// anyMessages reports whether numSet matches a message, with a UID SEARCH
// sent only if check succeeds.
func (c *Client) anyMessages(numSet imap.NumSet, check func() error) (bool, error) {
	var criteria imap.SearchCriteria
	switch numSet := numSet.(type) {
	case imap.UIDSet:
		criteria.UID = []imap.UIDSet{numSet}
	case imap.SeqSet:
		criteria.SeqNum = []imap.SeqSet{numSet}
	}
	cmd, err := c.searchIf(imapwire.NumKindUID, &criteria, nil, check)
	if err != nil {
		return false, err
	}
	data, err := cmd.Wait()
	if err != nil {
		return false, err
	}
	uids, _ := data.All.(imap.UIDSet)
	return len(uids) > 0, nil
}

// MoveCommand is a MOVE command.
type MoveCommand struct {
	commandBase
	data   MoveData
	copied bool // the fallback's COPY succeeded
}

// Wait blocks until the command has completed.
//
// If the fallback fails after COPY, the messages may be left in both
// mailboxes, and the MoveData is returned along with the error. Otherwise, the
// MoveData is nil on error.
func (cmd *MoveCommand) Wait() (*MoveData, error) {
	err := cmd.wait()
	if err != nil && !cmd.copied {
		return nil, err
	}
	return &cmd.data, err
}

// MoveData contains the data returned by a MOVE command.
type MoveData struct {
	// requires UIDPLUS or IMAP4rev2
	UIDValidity uint32
	SourceUIDs  imap.NumSet
	DestUIDs    imap.NumSet
}
