package imapclient

import (
	"fmt"

	"github.com/emersion/go-sasl"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/internal"
)

// Authenticate sends an AUTHENTICATE command.
//
// Unlike other commands, this method blocks until the SASL exchange completes.
//
// If the exchange fails on the client side, for instance because the
// mechanism's Next returns an error, it is cancelled. The returned error then
// wraps both the mechanism's error and the server's response, so check it with
// errors.Is or errors.As.
func (c *Client) Authenticate(saslClient sasl.Client) error {
	mech, initialResp, err := saslClient.Start()
	if err != nil {
		return err
	}

	// c.Caps may send a CAPABILITY command, so check it before c.beginCommand
	var hasSASLIR bool
	if initialResp != nil {
		hasSASLIR = c.Caps().Has(imap.CapSASLIR)
	}

	cmd := &authenticateCommand{}
	contReq := c.registerContReq(cmd)
	enc := c.beginCommand("AUTHENTICATE", cmd)
	enc.SP().Atom(mech)
	if initialResp != nil && hasSASLIR {
		enc.SP().Atom(internal.EncodeSASL(initialResp))
		initialResp = nil
	}
	enc.flush()
	defer enc.end()

	for {
		challengeStr, err := contReq.Wait()
		if err != nil {
			return cmd.wait()
		}

		if challengeStr == "" {
			if initialResp == nil {
				return c.cancelSASL(cmd, fmt.Errorf("imapclient: server requested SASL initial response, but we don't have one"))
			}

			contReq = c.registerContReq(cmd)
			if err := c.writeSASLLine(cmd, internal.EncodeSASL(initialResp)); err != nil {
				return err
			}
			initialResp = nil
			continue
		}

		challenge, err := internal.DecodeSASL(challengeStr)
		if err != nil {
			return c.cancelSASL(cmd, err)
		}

		resp, err := saslClient.Next(challenge)
		if err != nil {
			return c.cancelSASL(cmd, err)
		}

		contReq = c.registerContReq(cmd)
		if err := c.writeSASLLine(cmd, internal.EncodeSASL(resp)); err != nil {
			return err
		}
	}
}

type authenticateCommand struct {
	commandBase
}

// cancelSASL cancels the SASL exchange after a continuation request, so that
// the server leaves it and the connection stays usable. It returns err, with
// the server's response (usually NO or BAD) attached.
func (c *Client) cancelSASL(cmd *authenticateCommand, err error) error {
	cancelErr := c.writeSASLLine(cmd, "*")
	if cancelErr == nil {
		cancelErr = cmd.wait()
	}
	if cancelErr == nil {
		// The server completed an exchange the client gave up on, and the
		// client state is now authenticated. Don't keep that connection.
		c.closeWithError(err)
		return err
	}
	return fmt.Errorf("%w: %w", err, cancelErr)
}

// writeSASLLine writes a line of the SASL exchange. If the server has already
// ended the command, nothing is written, since the server would read the line
// as a new command. If the write fails, the server is left inside the exchange,
// so the connection is closed.
func (c *Client) writeSASLLine(cmd *authenticateCommand, s string) error {
	c.mutex.Lock()
	completed := cmd.completed
	c.mutex.Unlock()
	if completed {
		return nil
	}

	// The exchange can outlast the deadline set by beginCommand
	c.setWriteTimeout(cmdWriteTimeout)
	c.bw.WriteString(s)
	c.bw.WriteString("\r\n")
	if err := c.bw.Flush(); err != nil { // also reports WriteString errors
		c.closeWithError(err)
		return err
	}
	return nil
}

// Unauthenticate sends an UNAUTHENTICATE command.
//
// This command requires support for the UNAUTHENTICATE extension.
func (c *Client) Unauthenticate() *Command {
	cmd := &unauthenticateCommand{}
	c.beginCommand("UNAUTHENTICATE", cmd).end()
	return &cmd.Command
}

type unauthenticateCommand struct {
	Command
}
