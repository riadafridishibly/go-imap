package imapclient

import (
	"bufio"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/internal/imapwire"
)

var (
	testBodyTextPart = &imap.BodyStructureSinglePart{
		Type:     "TEXT",
		Subtype:  "PLAIN",
		Params:   map[string]string{"charset": "UTF-8"},
		Encoding: "7BIT",
		Size:     12,
		Text:     &imap.BodyStructureText{NumLines: 1},
		Extended: &imap.BodyStructureSinglePartExt{},
	}
	testBodyInnerPart = &imap.BodyStructureSinglePart{
		Type:     "TEXT",
		Subtype:  "PLAIN",
		Encoding: "7BIT",
		Size:     10,
		Text:     &imap.BodyStructureText{NumLines: 1},
	}
	testBodyMixedExt = &imap.BodyStructureMultiPartExt{
		Params: map[string]string{"boundary": "b"},
	}
)

const testBodyTextPartData = `("TEXT" "PLAIN" ("CHARSET" "UTF-8") NIL NIL "7BIT" 12 1 NIL NIL NIL NIL)`

var bodyStructureTests = []struct {
	name string
	data string
	want imap.BodyStructure
}{
	{
		name: "message/rfc822",
		data: `(` + testBodyTextPartData + `("MESSAGE" "RFC822" NIL NIL NIL "7BIT" 120 (NIL "hi" NIL NIL NIL NIL NIL NIL NIL NIL) ("TEXT" "PLAIN" NIL NIL NIL "7BIT" 10 1) 4 NIL NIL NIL NIL) "MIXED" ("BOUNDARY" "b") NIL NIL NIL)`,
		want: &imap.BodyStructureMultiPart{
			Children: []imap.BodyStructure{
				testBodyTextPart,
				&imap.BodyStructureSinglePart{
					Type:     "MESSAGE",
					Subtype:  "RFC822",
					Encoding: "7BIT",
					Size:     120,
					MessageRFC822: &imap.BodyStructureMessageRFC822{
						Envelope:      &imap.Envelope{Subject: "hi"},
						BodyStructure: testBodyInnerPart,
						NumLines:      4,
					},
					Extended: &imap.BodyStructureSinglePartExt{},
				},
			},
			Subtype:  "MIXED",
			Extended: testBodyMixedExt,
		},
	},
	{
		name: "message/global, IMAP4rev1 form",
		data: `(` + testBodyTextPartData + `("MESSAGE" "GLOBAL" ("NAME" "a.dat") NIL NIL "7BIT" 120 NIL ("ATTACHMENT" ("FILENAME" "a.dat")) NIL NIL) "MIXED" ("BOUNDARY" "b") NIL NIL NIL)`,
		want: &imap.BodyStructureMultiPart{
			Children: []imap.BodyStructure{
				testBodyTextPart,
				&imap.BodyStructureSinglePart{
					Type:     "MESSAGE",
					Subtype:  "GLOBAL",
					Params:   map[string]string{"name": "a.dat"},
					Encoding: "7BIT",
					Size:     120,
					Extended: &imap.BodyStructureSinglePartExt{
						Disposition: &imap.BodyStructureDisposition{
							Value:  "ATTACHMENT",
							Params: map[string]string{"filename": "a.dat"},
						},
					},
				},
			},
			Subtype:  "MIXED",
			Extended: testBodyMixedExt,
		},
	},
	{
		name: "message/global, IMAP4rev1 form, NIL disposition",
		data: `("MESSAGE" "GLOBAL" NIL NIL NIL "7BIT" 120 NIL NIL NIL NIL)`,
		want: &imap.BodyStructureSinglePart{
			Type:     "MESSAGE",
			Subtype:  "GLOBAL",
			Encoding: "7BIT",
			Size:     120,
			Extended: &imap.BodyStructureSinglePartExt{},
		},
	},
	{
		name: "message/global, IMAP4rev1 form, MD5 only",
		data: `("MESSAGE" "GLOBAL" NIL NIL NIL "7BIT" 120 NIL)`,
		want: &imap.BodyStructureSinglePart{
			Type:     "MESSAGE",
			Subtype:  "GLOBAL",
			Encoding: "7BIT",
			Size:     120,
			Extended: &imap.BodyStructureSinglePartExt{},
		},
	},
	{
		name: "message/rfc822, no envelope, body and line count",
		data: `(` + testBodyTextPartData + `("MESSAGE" "RFC822" NIL NIL NIL "7BIT" 120 NIL NIL NIL NIL) "MIXED" ("BOUNDARY" "b") NIL NIL NIL)`,
		want: &imap.BodyStructureMultiPart{
			Children: []imap.BodyStructure{
				testBodyTextPart,
				&imap.BodyStructureSinglePart{
					Type:     "MESSAGE",
					Subtype:  "RFC822",
					Encoding: "7BIT",
					Size:     120,
					Extended: &imap.BodyStructureSinglePartExt{},
				},
			},
			Subtype:  "MIXED",
			Extended: testBodyMixedExt,
		},
	},
	{
		name: "message/rfc822, NIL envelope",
		data: `(` + testBodyTextPartData + `("MESSAGE" "RFC822" NIL NIL NIL "7BIT" 120 NIL ("TEXT" "PLAIN" NIL NIL NIL "7BIT" 10 1) 4 NIL NIL NIL NIL) "MIXED" ("BOUNDARY" "b") NIL NIL NIL)`,
		want: &imap.BodyStructureMultiPart{
			Children: []imap.BodyStructure{
				testBodyTextPart,
				&imap.BodyStructureSinglePart{
					Type:     "MESSAGE",
					Subtype:  "RFC822",
					Encoding: "7BIT",
					Size:     120,
					MessageRFC822: &imap.BodyStructureMessageRFC822{
						BodyStructure: testBodyInnerPart,
						NumLines:      4,
					},
					Extended: &imap.BodyStructureSinglePartExt{},
				},
			},
			Subtype:  "MIXED",
			Extended: testBodyMixedExt,
		},
	},
	{
		name: "message/rfc822, NIL envelope, multipart body",
		data: `("MESSAGE" "RFC822" NIL NIL NIL "7BIT" 120 NIL (("TEXT" "PLAIN" NIL NIL NIL "7BIT" 10 1)("TEXT" "PLAIN" NIL NIL NIL "7BIT" 10 1) "MIXED") 4)`,
		want: &imap.BodyStructureSinglePart{
			Type:     "MESSAGE",
			Subtype:  "RFC822",
			Encoding: "7BIT",
			Size:     120,
			MessageRFC822: &imap.BodyStructureMessageRFC822{
				BodyStructure: &imap.BodyStructureMultiPart{
					Children: []imap.BodyStructure{testBodyInnerPart, testBodyInnerPart},
					Subtype:  "MIXED",
				},
				NumLines: 4,
			},
		},
	},
	{
		name: "multipart with no children",
		// https://github.com/emersion/go-imap/issues/701
		data: `(("ALTERNATIVE" ("BOUNDARY" "e7e3f78cb2203c4e50561d13c480670d46c0bb5ccba09400aae9c221fc41") NIL NIL)("APPLICATION" "PDF" NIL NIL NIL "BASE64" 1476806 NIL ("ATTACHMENT" ("FILENAME" "investing-101.pdf")) NIL) "MIXED" ("BOUNDARY" "7b1fa0fc24c33f620e26ce902dcf2ecbcc7ba56cd9e042624b2ee60b9d56") NIL NIL)`,
		want: &imap.BodyStructureMultiPart{
			Children: []imap.BodyStructure{
				&imap.BodyStructureMultiPart{
					Subtype: "ALTERNATIVE",
					Extended: &imap.BodyStructureMultiPartExt{
						Params: map[string]string{"boundary": "e7e3f78cb2203c4e50561d13c480670d46c0bb5ccba09400aae9c221fc41"},
					},
				},
				&imap.BodyStructureSinglePart{
					Type:     "APPLICATION",
					Subtype:  "PDF",
					Encoding: "BASE64",
					Size:     1476806,
					Extended: &imap.BodyStructureSinglePartExt{
						Disposition: &imap.BodyStructureDisposition{
							Value:  "ATTACHMENT",
							Params: map[string]string{"filename": "investing-101.pdf"},
						},
					},
				},
			},
			Subtype: "MIXED",
			Extended: &imap.BodyStructureMultiPartExt{
				Params: map[string]string{"boundary": "7b1fa0fc24c33f620e26ce902dcf2ecbcc7ba56cd9e042624b2ee60b9d56"},
			},
		},
	},
	{
		name: "multipart with no children and no extension data",
		data: `(("ALTERNATIVE")("TEXT" "PLAIN" NIL NIL NIL "7BIT" 10 1) "MIXED")`,
		want: &imap.BodyStructureMultiPart{
			Children: []imap.BodyStructure{
				&imap.BodyStructureMultiPart{Subtype: "ALTERNATIVE"},
				testBodyInnerPart,
			},
			Subtype: "MIXED",
		},
	},
}

func TestReadBody(t *testing.T) {
	for _, tc := range bodyStructureTests {
		t.Run(tc.name, func(t *testing.T) {
			dec := imapwire.NewDecoder(bufio.NewReader(iotest.OneByteReader(strings.NewReader(tc.data+"\r\n"))), imapwire.ConnSideClient)
			got, err := readBody(dec, &Options{})
			if err != nil {
				t.Fatalf("readBody() = %v", err)
			}
			if !dec.ExpectCRLF() {
				t.Fatalf("ExpectCRLF() = %v", dec.Err())
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("readBody() = %s, want %s", toJSON(got), toJSON(tc.want))
			}
		})
	}
}

// Dovecot sends message/global parts in the IMAP4rev1 form. See:
// https://github.com/emersion/go-imap/issues/678
func TestReadBody_dovecotMessageGlobal(t *testing.T) {
	const data = `(((("text" "plain" ("charset" "UTF-8") NIL NIL "quoted-printable" 576 31 NIL NIL NIL NIL)("text" "html" ("charset" "UTF-8") NIL NIL "quoted-printable" 4691 112 NIL NIL NIL NIL) "alternative" ("boundary" "----=_NextPart_002_0063_01D85E63.43189F50") NIL NIL NIL)("image" "png" ("name" "image001.png") "<image001.png@01D85E63.36D4F950>" NIL "base64" 3832 NIL NIL NIL NIL) "related" ("boundary" "----=_NextPart_001_0062_01D85E63.43189F50") NIL NIL NIL)("message" "delivery-status" ("name" "details.txt") NIL NIL "7bit" 594 NIL ("attachment" ("filename" "details.txt")) NIL NIL)("message" "global" ("name" "Untitled attachment 00019.dat") NIL NIL "7bit" 6726 NIL ("attachment" ("filename" "Untitled attachment 00019.dat")) NIL NIL) "mixed" ("boundary" "----=_NextPart_000_0061_01D85E63.43189F50") NIL ("en-us") NIL)`

	dec := imapwire.NewDecoder(bufio.NewReader(strings.NewReader(data+"\r\n")), imapwire.ConnSideClient)
	bs, err := readBody(dec, &Options{})
	if err != nil {
		t.Fatalf("readBody() = %v", err)
	}

	mpart, ok := bs.(*imap.BodyStructureMultiPart)
	if !ok || len(mpart.Children) != 3 {
		t.Fatalf("readBody() = %s, want multipart with 3 children", toJSON(bs))
	}
	want := &imap.BodyStructureSinglePart{
		Type:     "message",
		Subtype:  "global",
		Params:   map[string]string{"name": "Untitled attachment 00019.dat"},
		Encoding: "7bit",
		Size:     6726,
		Extended: &imap.BodyStructureSinglePartExt{
			Disposition: &imap.BodyStructureDisposition{
				Value:  "attachment",
				Params: map[string]string{"filename": "Untitled attachment 00019.dat"},
			},
		},
	}
	if got := mpart.Children[2]; !reflect.DeepEqual(got, want) {
		t.Errorf("part 3 = %s, want %s", toJSON(got), toJSON(want))
	}
}

func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
