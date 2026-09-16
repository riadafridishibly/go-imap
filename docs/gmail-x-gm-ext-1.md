# Gmail X-GM-EXT-1: observed wire behavior

What Gmail's IMAP server actually sends and accepts for `X-GM-MSGID`,
`X-GM-THRID`, `X-GM-LABELS` and `X-GM-RAW`, where Google's documentation is
silent or differs from what the server does.

Google's reference: https://developers.google.com/workspace/gmail/imap/imap-extensions

## How this was tested

- Date: 2026-09-16, against `imap.gmail.com:993`.
- A throwaway account with 651 messages, logged in with `LOGIN` and an app
  password.
- A raw TLS client that tags commands and prints both sides. `imapclient` could
  not be used: it closes the connection on the first `X-GM-*` attribute (#3).
- Labels were created, applied to one message, and removed again afterwards.
- To see how ids are formed, messages were appended to `INBOX` with chosen
  dates and deleted through `[Gmail]/Trash` afterwards.
- Excerpts below are copied from the transcripts, with tags renumbered and
  unrelated lines left out.

## Capabilities

Before authentication:

```
IMAP4rev1 UNSELECT IDLE NAMESPACE QUOTA ID XLIST CHILDREN X-GM-EXT-1 XYZZY SASL-IR AUTH=XOAUTH2 AUTH=PLAIN AUTH=PLAIN-CLIENTTOKEN AUTH=OAUTHBEARER
```

After authentication:

```
IMAP4rev1 UNSELECT IDLE NAMESPACE QUOTA ID XLIST CHILDREN X-GM-EXT-1 UIDPLUS COMPRESS=DEFLATE ENABLE MOVE CONDSTORE ESEARCH UTF8=ACCEPT LIST-EXTENDED LIST-STATUS LITERAL- SPECIAL-USE APPENDLIMIT=35651584
```

Notes:

- `UTF8=ACCEPT` is advertised, and it changes how label names are encoded (see
  below).
- `LITERAL-` is advertised, `LITERAL+` is not. A non-synchronizing literal is
  limited to 4096 bytes; anything larger needs the `+ go ahead` round trip.
- No `SORT`, `THREAD` or `OBJECTID`.

## FETCH

### Attribute order

Gmail picks its own order, and `UID` comes after the Gmail items:

```
C: a1 UID FETCH 1:* (UID X-GM-MSGID X-GM-THRID X-GM-LABELS)
S: * 1 FETCH (X-GM-THRID 1873891894758975366 X-GM-MSGID 1873891894758975366 X-GM-LABELS ("\\Inbox") UID 1)
S: * 2 FETCH (X-GM-THRID 1873891894758975366 X-GM-MSGID 1873892116380601938 X-GM-LABELS ("\\Inbox") UID 2)

C: a2 UID FETCH 636 (FLAGS MODSEQ X-GM-LABELS)
S: * 636 FETCH (X-GM-LABELS () UID 636 MODSEQ (18964) FLAGS ())
```

All 651 messages came back as `X-GM-THRID X-GM-MSGID X-GM-LABELS UID`.

### Message and thread ids

- Unsigned decimal, as documented.
- For 646 of 651 messages `X-GM-THRID` equals `X-GM-MSGID`. Where they differ,
  the thread id is another message's `X-GM-MSGID`, consistent with a thread
  taking the id of its first message.

### Id layout

Google does not document it, but the top 44 bits of `X-GM-MSGID` are the
message's INTERNALDATE in milliseconds since the Unix epoch:

```
1876409439502535953 >> 20 = 1789483489515 = 2026-09-15 14:44:49.515 UTC
```

- For all 651 messages, `id >> 20` matched INTERNALDATE: 595 to the second, the
  other 56 within 1.33 s. INTERNALDATE has one-second precision; the id also
  carries milliseconds.
- The id follows INTERNALDATE, not the time Gmail stored the message. A message
  appended with the date `"01-Jan-2001 00:00:00 +0000"` got an old id:

  ```
  S: * 12 FETCH (X-GM-THRID 1025829451352874949 X-GM-MSGID 1025829451352874949 UID 12 INTERNALDATE "01-Jan-2001 00:00:00 +0000")
  ```

  `1025829451352874949 >> 20` is 2001-01-01 00:00:00.768 UTC.
- APPEND dates before 1970 or in the future are replaced with the current time,
  and the id follows. Dates in 1900, 1960, 2027 (3.5 months ahead), 2249 and
  2300 were all stored as the time of the APPEND. `01-Jan-1970 00:00:00 +0000`
  was kept:

  ```
  S: * 13 FETCH (X-GM-THRID 161198677 X-GM-MSGID 161198677 UID 16 INTERNALDATE "01-Jan-1970 00:00:00 +0000")
  ```

- Ids are therefore not in arrival order.
- With INTERNALDATE held between 1970 and now, an id stays below `now << 20`.
  The first id above `math.MaxInt64` would belong to a message dated
  2248-09-26 or later.

### Label list forms

Counts over the 651 messages in `[Gmail]/All Mail`:

```
611 X-GM-LABELS ()
 29 X-GM-LABELS ("\\Sent")
  5 X-GM-LABELS ("\\Inbox")
  5 X-GM-LABELS ("\\Important" "\\Inbox")
  1 X-GM-LABELS ("\\Draft")
```

- System labels are always sent quoted, with the backslash escaped:
  `"\\Inbox"`. The bare `\Inbox` form in Google's documentation did not appear.
- User labels are sent as bare atoms when they can be (`ProbeImplicit`,
  `&ANw-bung`, `R&-D`) and quoted otherwise (`"work/project x"`).
- An empty list is `()`. `NIL` was not seen.
- No label arrived as a literal.

### A label folder's view omits its own label

`X-GM-LABELS` leaves out the label of the selected mailbox. Only
`[Gmail]/All Mail` returns the full set.

```
C: a1 SELECT "&ANw-bung"
S: * 1 EXISTS
C: a2 FETCH 1:* (UID X-GM-LABELS)
S: * 1 FETCH (X-GM-LABELS ("\\Starred" "\\foo" "work/project x" ProbeImplicit R&-D) UID 2)
```

The same message in `[Gmail]/All Mail` also carries `&ANw-bung`. Messages
fetched from `INBOX` showed `X-GM-LABELS ()` although they carry `\Inbox`.

## System labels and user labels look the same

Gmail accepts user labels whose names start with a backslash, including the
names of system labels:

```
C: a1 CREATE "\\foo"
S: a1 OK Success
C: a2 CREATE "\\Starred"
S: a2 OK Success
C: a3 CREATE "\\Inbox"
S: a3 OK Success
```

On the wire the user label `\foo` is indistinguishable from the system label
`\Starred`:

```
S: * 636 FETCH (X-GM-LABELS ("\\Starred" "\\foo" "work/project x" &ANw-bung ProbeImplicit R&-D) UID 636)
```

When a user label shares a system label's name, `X-GM-LABELS` resolves the name
to the system label. After `UID STORE 636 +X-GM-LABELS ("\\Starred")` the
message gained `\Flagged`, and `EXAMINE "\\Starred"` (the user label) still
reported `0 EXISTS`. The user label cannot be addressed through `X-GM-LABELS`.

A client therefore cannot tell system labels from user labels by their form.
The set of system names is Gmail's: `\Inbox`, `\Sent`, `\Draft`, `\Important`
and `\Starred` were observed.

## Label name encoding

The encoding depends on the command and on whether `UTF8=ACCEPT` is enabled.
The labels used were `Übung` (non-ASCII), `R&D` (ampersand) and
`work/project x` (nested, with a space).

| Context | Default mode | After `ENABLE UTF8=ACCEPT` |
|---|---|---|
| FETCH and STORE replies | modified UTF-7: `&ANw-bung`, `R&-D` | raw UTF-8: `"Übung"`, `R&D` |
| STORE arguments | modified UTF-7 | raw UTF-8, `&` is literal |
| SEARCH `X-GM-LABELS` | decoded name, `CHARSET UTF-8` for non-ASCII | decoded name |
| LIST, CREATE, DELETE | modified UTF-7 | raw UTF-8, `&` is literal |

### STORE and FETCH

Default mode:

```
C: a1 UID STORE 636 +X-GM-LABELS (&ANw-bung)
S: * 636 FETCH (X-GM-LABELS (&ANw-bung) MODSEQ (20494) UID 636)
```

With `UTF8=ACCEPT` the same labels come back decoded, and a UTF-8 name in STORE
addresses the existing label rather than creating a new one:

```
C: a1 ENABLE UTF8=ACCEPT
S: * ENABLED UTF8=ACCEPT
C: a2 UID FETCH 636 (X-GM-LABELS)
S: * 636 FETCH (X-GM-LABELS ("\\foo" "work/project x" "Übung" ProbeImplicit) UID 636)
C: a3 UID STORE 636 -X-GM-LABELS ("Übung")
S: * 636 FETCH (X-GM-LABELS ("\\foo" "work/project x" ProbeImplicit) UID 636)
```

In `UTF8=ACCEPT` mode `&` has no special meaning. `R&-D` there is a different
label from `R&D`, and STORE creates it:

```
C: a1 UID STORE 636 +X-GM-LABELS ("R&-D")
S: * 636 FETCH (X-GM-LABELS ("\\Starred" "\\foo" "work/project x" "Übung" ProbeImplicit R&-D) UID 636)
C: a2 LIST "" "*"
S: * LIST (\HasNoChildren) "/" "R&-D"
S: * LIST (\HasNoChildren) "/" "R&D"
```

### SEARCH

`X-GM-LABELS` in SEARCH takes the decoded name in both modes. The modified
UTF-7 form matches nothing:

```
C: a1 UID SEARCH X-GM-LABELS &ANw-bung
S: * SEARCH
C: a2 UID SEARCH X-GM-LABELS "R&D"
S: * SEARCH 636
C: a3 UID SEARCH X-GM-LABELS "R&-D"
S: * SEARCH
```

(In this default-mode session the only such label was `R&D`, sent by FETCH as
`R&-D`.)

A non-ASCII name in default mode must be a literal with `CHARSET UTF-8`. Raw
UTF-8 in a quoted string is rejected:

```
C: a1 UID SEARCH X-GM-LABELS "Übung"
S: a1 BAD Could not parse command
C: a2 UID SEARCH CHARSET UTF-8 X-GM-LABELS {6}
S: + go ahead
C: Übung
S: * SEARCH 636
```

With `UTF8=ACCEPT`, `UID SEARCH X-GM-LABELS "Übung"` matches directly.

System labels must be quoted in SEARCH. STORE accepts both forms:

```
C: a1 UID SEARCH X-GM-LABELS \Inbox
S: a1 BAD Could not parse command
C: a2 UID SEARCH X-GM-LABELS "\\Inbox"
S: * SEARCH 1 2 3 4 639 646 647 648 650 651
```

## STORE X-GM-LABELS

### Accepted forms

Both the bare and the quoted system label work:

```
C: a1 UID STORE 636 +X-GM-LABELS (\Starred)
C: a2 UID STORE 636 -X-GM-LABELS ("\\Starred")
C: a3 UID STORE 636 +X-GM-LABELS ("\\Important")
C: a4 UID STORE 636 -X-GM-LABELS (\Important)
```

All four returned `OK Success` with the expected label list.

### Reply shape

- `UID STORE` replies carry `UID`, after `X-GM-LABELS` (and after `MODSEQ` once
  CONDSTORE is enabled).
- Plain `STORE` replies carry no `UID`:

  ```
  C: a1 STORE 636 +X-GM-LABELS (ProbeImplicit)
  S: * 636 FETCH (X-GM-LABELS ("\\foo" "work/project x" &ANw-bung ProbeImplicit) MODSEQ (20579))
  ```

- Adding or removing `\Starred` also changes `\Flagged`, and Gmail sends a
  second FETCH for the same message:

  ```
  C: a1 UID STORE 636 +X-GM-LABELS (\Starred)
  S: * 636 FETCH (X-GM-LABELS ("\\Starred" "work/project x" &ANw-bung) MODSEQ (20509) UID 636)
  S: * 636 FETCH (UID 636 MODSEQ (20509) FLAGS (\Flagged))
  ```

### Unknown labels are created

STORE with a label that does not exist creates it. A misspelled label name
becomes a new label.

```
C: a1 UID STORE 636 +X-GM-LABELS (ProbeImplicit)
S: * 636 FETCH (X-GM-LABELS ("\\foo" "work/project x" &ANw-bung ProbeImplicit) MODSEQ (20566) UID 636)
C: a2 LIST "" "*"
S: * LIST (\HasNoChildren) "/" "ProbeImplicit"
```

### .SILENT and UNCHANGEDSINCE

Both work. `.SILENT` suppresses the untagged FETCH (tested with removal):

```
C: a1 UID STORE 636 -X-GM-LABELS.SILENT ("work/project x")
S: a1 OK Success
```

With a stale value, `UNCHANGEDSINCE` fails the store with `MODIFIED` and
returns the current labels (the success path was not tested):

```
C: a1 UID STORE 636 (UNCHANGEDSINCE 1) +X-GM-LABELS ("work/project x")
S: * 636 FETCH (X-GM-LABELS ("\\foo" &ANw-bung ProbeImplicit) MODSEQ (20572) UID 636)
S: a1 OK [MODIFIED 636] Conditional Store Failed (Success)
```

### Removing the selected folder's label does nothing

With a label's folder selected, removing that label from a message returns
`OK Success` but leaves the label in place. No EXPUNGE is sent, even after
NOOP. Observed twice, in default mode and with `UTF8=ACCEPT`. The excerpt is
from the `UTF8=ACCEPT` session:

```
C: a1 SELECT "Übung"
S: * 1 EXISTS
C: a2 STORE 1 -X-GM-LABELS ("Übung")
S: * 1 FETCH (X-GM-LABELS ("\\Starred" "\\foo" "work/project x" ProbeImplicit R&-D))
S: a2 OK Success
C: a3 NOOP
S: a3 OK Success
C: a4 SELECT "[Gmail]/All Mail"
C: a5 UID FETCH 636 (FLAGS X-GM-LABELS)
S: * 636 FETCH (X-GM-LABELS ("\\Starred" "\\foo" "work/project x" "Übung" ProbeImplicit R&-D) UID 636 FLAGS (\Flagged))
```

The same removal from `[Gmail]/All Mail` works.

## SEARCH keys

```
C: a1 UID SEARCH X-GM-MSGID 1853070351386201256
S: * SEARCH 636
C: a2 UID SEARCH X-GM-THRID 1853070351386201256
S: * SEARCH 636
C: a3 UID SEARCH X-GM-RAW "label:work-project-x"
S: * SEARCH 636
```

- `X-GM-RAW` uses web search syntax, where a nested label `work/project x` is
  written `work-project-x`.
- Non-ASCII in `X-GM-RAW` follows the same rule as `X-GM-LABELS`: in default
  mode a quoted string gets `BAD Could not parse command`, and
  `CHARSET UTF-8 X-GM-RAW {12}` with a literal works. With `UTF8=ACCEPT` a
  quoted UTF-8 string works.

## Mailboxes

- Creating `work/project x` also creates `work` as `\Noselect`. Deleting the
  child removes the parent: a later `DELETE "work"` returned
  `NO [NONEXISTENT] Unknown folder.`
- With `UTF8=ACCEPT`, mailbox names follow the label rules above: `LIST` returns
  `"Übung"` and `"R&D"` raw, and `R&-D` is a separate mailbox.

## Consequences for imapclient

- Labels should be exposed as plain strings. There is no reliable way to mark
  one as a system label, and `imap.Flag` canonicalization should not be applied.
- The label reader must accept quoted strings and bare atoms, including a
  leading backslash, and must know whether `UTF8=ACCEPT` is enabled: modified
  UTF-7 decoding rejects a raw `R&D`.
- STORE should always send quoted strings: modified UTF-7 in default mode, raw
  UTF-8 with `UTF8=ACCEPT`.
- SEARCH must send the decoded name, and a non-ASCII label or `X-GM-RAW` query
  must set `CHARSET UTF-8` in default mode.
- Replies cannot be relied on to put `UID` first.
- Ids should be `uint64`, as documented. Real ids fit in `int64` under the
  layout above, but the layout is undocumented, so a test for a value above
  `math.MaxInt64` has to use a synthetic id.
- Sorting by `X-GM-MSGID` does not give arrival order.
- Existing bug, separate from the X-GM work (#20): with `UTF8=ACCEPT` enabled,
  `Encoder.Mailbox` escapes `&` as `&-`, so `Create("Q&A")` creates a mailbox
  named `Q&-A` on Gmail, and `ExpectMailbox` runs `utf7.Decode` on every name,
  so a LIST that returns `"R&D"` fails with `utf7: invalid UTF-7` and closes the
  connection. Both were reproduced through `imapclient`.

## Not tested

- `NIL` in place of a label list.
- A label sent as a literal.
- Label names containing `"` or `\` after the first character.
- How far in the future an APPEND date can be before Gmail replaces it. A date
  3.5 months ahead was replaced.
- Unsolicited `X-GM-*` attributes, for example during IDLE.
- XOAUTH2 login.
