# go-imap

[![Go Reference](https://pkg.go.dev/badge/github.com/emersion/go-imap/v2.svg)](https://pkg.go.dev/github.com/emersion/go-imap/v2)

An [IMAP4rev2] library for Go.

This is a fork of [emersion/go-imap]. See [About this fork](#about-this-fork)
before using it in place of upstream.

> **Note**
> This is the README for go-imap v2. This new major version is still in
> development. For go-imap v1, see the [v1 branch].

## About this fork

The fork is based on upstream `v2`, last synced at commit `dd8c632`
(2026-10-22), and is maintained separately. It may not stay compatible with
upstream:

- The API and behavior can diverge. Changes here are not sent upstream, and
  upstream changes are not merged on a schedule.
- If upstream later adds one of the features below, the two versions may use
  different names or types.
- The module path is still `github.com/emersion/go-imap/v2`, so a build uses
  either the fork or upstream, never both.

### What it adds

- Gmail `X-GM-EXT-1` in `imapclient`. Wire behavior is recorded in
  [docs/gmail-x-gm-ext-1.md](docs/gmail-x-gm-ext-1.md).
  - FETCH message id, thread id and labels: `FetchOptions.GmailMsgID`,
    `GmailThreadID` and `GmailLabels`.
  - STORE labels: `Client.StoreGmailLabels`.
  - SEARCH by message id, thread id, label and Gmail web search query:
    `SearchCriteria.GmailMsgID`, `GmailThreadID`, `GmailLabels` and `GmailRaw`.
- BODYSTRUCTURE variants sent by real servers: `message/rfc822` and
  `message/global` parts without the envelope, body and line count, a `NIL`
  envelope, and a multipart with no parts. Nesting is limited to 1000 levels,
  and `imapserver` can write a multipart with no parts.
- FETCH, STATUS and SEARCH RETURN items are written in a fixed order.
- `CAPABILITY` is sent at fixed points, not from a background goroutine:
  - `Login(...).Wait()`, `Unauthenticate().Wait()`, `Authenticate` and
    `NewStartTLS` request the new list before returning, if the server didn't
    send it.
  - After a greeting without capabilities, the first `Caps` call requests them.
  - `Caps` called while one of those commands is pending waits for the new list.
- `SearchCriteria.And` keeps `Smaller` when the other criteria have none.

### Differences that can break upstream code

- Go 1.27 or later is required (upstream: Go 1.18).
- `BodyStructureMessageRFC822.Envelope` can be nil,
  `BodyStructureSinglePart.MessageRFC822` can be nil for more responses, and
  `BodyStructureMultiPart.Children` can be empty.
- When a SASL exchange fails on the client side, `Client.Authenticate` cancels
  it, and the returned error wraps the mechanism's error and the server's
  response. Compare it with `errors.Is` or `errors.As`, not `==` or a type
  assertion.
- `Client.Move` returns an error and sends nothing if the capabilities can't
  be fetched (upstream falls back as if the server had no MOVE).
- `Client.Move` on a server without MOVE (upstream sends all three fallback
  commands at once, deletes the messages even if COPY fails, and uses plain
  `EXPUNGE`):
  - Requires UIDPLUS. Without it, returns an error and sends nothing.
  - Refuses a read-only mailbox before sending anything.
  - Sends each command after the previous one succeeds, and blocks. Don't call
    it while another command's responses are being consumed.
  - Removes only the messages listed in COPY's `COPYUID`. Without `COPYUID`,
    they are copied and left in place.
  - Stops after COPY if a `SELECT` or `EXAMINE` is sent before STORE or
    EXPUNGE, for example from another goroutine.

## Usage

Because the module path is unchanged, replace upstream with the fork in your
`go.mod`:

    go get github.com/emersion/go-imap/v2
    go mod edit -replace github.com/emersion/go-imap/v2=github.com/riadafridishibly/go-imap/v2@<commit>
    go mod tidy

`<commit>` is a commit hash from the `v2` branch. `go mod tidy` turns it into a
pseudo-version. A `replace` only applies to the main module, so a library that
depends on the fork has to ask its users to add the same line.

Documentation and examples for the module are available here. They are
generated from upstream and do not list the additions above:

- [Client docs]
- [Server docs]

## Contributing

Open issues and PRs on [riadafridishibly/go-imap], not upstream.

## License

MIT

[IMAP4rev2]: https://www.rfc-editor.org/rfc/rfc9051.html
[emersion/go-imap]: https://github.com/emersion/go-imap
[v1 branch]: https://github.com/emersion/go-imap/tree/v1
[Client docs]: https://pkg.go.dev/github.com/emersion/go-imap/v2/imapclient
[Server docs]: https://pkg.go.dev/github.com/emersion/go-imap/v2/imapserver
[riadafridishibly/go-imap]: https://github.com/riadafridishibly/go-imap
