package imapclient

import (
	"bufio"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/riadafridishibly/go-imap/v2"
	"github.com/riadafridishibly/go-imap/v2/internal/imapwire"
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
		name: "multipart with no children, body-fld-param only",
		data: `(("ALTERNATIVE" ("BOUNDARY" "x"))("TEXT" "PLAIN" NIL NIL NIL "7BIT" 10 1) "MIXED")`,
		want: &imap.BodyStructureMultiPart{
			Children: []imap.BodyStructure{
				&imap.BodyStructureMultiPart{
					Subtype: "ALTERNATIVE",
					Extended: &imap.BodyStructureMultiPartExt{
						Params: map[string]string{"boundary": "x"},
					},
				},
				testBodyInnerPart,
			},
			Subtype: "MIXED",
		},
	},
	{
		name: "multipart with no children, full body-ext-mpart",
		data: `("ALTERNATIVE" ("BOUNDARY" "x") ("INLINE" NIL) ("EN") "loc")`,
		want: &imap.BodyStructureMultiPart{
			Subtype: "ALTERNATIVE",
			Extended: &imap.BodyStructureMultiPartExt{
				Params:      map[string]string{"boundary": "x"},
				Disposition: &imap.BodyStructureDisposition{Value: "INLINE"},
				Language:    []string{"EN"},
				Location:    "loc",
			},
		},
	},
	{
		name: "message/global, IMAP4rev1 form, MD5 string",
		data: `("MESSAGE" "GLOBAL" NIL NIL NIL "7BIT" 120 "abc" ("ATTACHMENT" NIL) ("EN" "FR") "loc")`,
		want: &imap.BodyStructureSinglePart{
			Type:     "MESSAGE",
			Subtype:  "GLOBAL",
			Encoding: "7BIT",
			Size:     120,
			Extended: &imap.BodyStructureSinglePartExt{
				Disposition: &imap.BodyStructureDisposition{Value: "ATTACHMENT"},
				Language:    []string{"EN", "FR"},
				Location:    "loc",
			},
		},
	},
	{
		name: "message/global, IMAP4rev1 form, language and location",
		data: `("MESSAGE" "GLOBAL" NIL NIL NIL "7BIT" 120 NIL ("ATTACHMENT" NIL) "EN" "loc")`,
		want: &imap.BodyStructureSinglePart{
			Type:     "MESSAGE",
			Subtype:  "GLOBAL",
			Encoding: "7BIT",
			Size:     120,
			Extended: &imap.BodyStructureSinglePartExt{
				Disposition: &imap.BodyStructureDisposition{Value: "ATTACHMENT"},
				Language:    []string{"EN"},
				Location:    "loc",
			},
		},
	},
	{
		name: "message/rfc822, NIL envelope, literal media type",
		data: "(\"MESSAGE\" \"RFC822\" NIL NIL NIL \"7BIT\" 120 NIL ({4}\r\nTEXT \"PLAIN\" NIL NIL NIL \"7BIT\" 10 1) 4)",
		want: &imap.BodyStructureSinglePart{
			Type:     "MESSAGE",
			Subtype:  "RFC822",
			Encoding: "7BIT",
			Size:     120,
			MessageRFC822: &imap.BodyStructureMessageRFC822{
				BodyStructure: testBodyInnerPart,
				NumLines:      4,
			},
		},
	},
	{
		name: "message/rfc822, NIL envelope, long media type",
		data: `("MESSAGE" "RFC822" NIL NIL NIL "7BIT" 120 NIL ("` + strings.Repeat("X", 5000) + `" "PLAIN" NIL NIL NIL "7BIT" 10) 4)`,
		want: &imap.BodyStructureSinglePart{
			Type:     "MESSAGE",
			Subtype:  "RFC822",
			Encoding: "7BIT",
			Size:     120,
			MessageRFC822: &imap.BodyStructureMessageRFC822{
				BodyStructure: &imap.BodyStructureSinglePart{
					Type:     strings.Repeat("X", 5000),
					Subtype:  "PLAIN",
					Encoding: "7BIT",
					Size:     10,
				},
				NumLines: 4,
			},
		},
	},
	{
		name: "message/rfc822, NIL envelope, multipart body with no children",
		data: `("MESSAGE" "RFC822" NIL NIL NIL "7BIT" 120 NIL ("ALTERNATIVE" ("BOUNDARY" "x") NIL NIL) 4)`,
		want: &imap.BodyStructureSinglePart{
			Type:     "MESSAGE",
			Subtype:  "RFC822",
			Encoding: "7BIT",
			Size:     120,
			MessageRFC822: &imap.BodyStructureMessageRFC822{
				BodyStructure: &imap.BodyStructureMultiPart{
					Subtype: "ALTERNATIVE",
					Extended: &imap.BodyStructureMultiPartExt{
						Params: map[string]string{"boundary": "x"},
					},
				},
				NumLines: 4,
			},
		},
	},
	{
		// ("ALTERNATIVE" ("BOUNDARY" "x")) could also be a body-fld-dsp, but
		// body-fld-lang can't be a number
		name: "message/rfc822, NIL envelope, multipart body with no children, body-fld-param only",
		data: `("MESSAGE" "RFC822" NIL NIL NIL "7BIT" 120 NIL ("ALTERNATIVE" ("BOUNDARY" "x")) 4 NIL ("INLINE" NIL) NIL NIL)`,
		want: &imap.BodyStructureSinglePart{
			Type:     "MESSAGE",
			Subtype:  "RFC822",
			Encoding: "7BIT",
			Size:     120,
			MessageRFC822: &imap.BodyStructureMessageRFC822{
				BodyStructure: &imap.BodyStructureMultiPart{
					Subtype: "ALTERNATIVE",
					Extended: &imap.BodyStructureMultiPartExt{
						Params: map[string]string{"boundary": "x"},
					},
				},
				NumLines: 4,
			},
			Extended: &imap.BodyStructureSinglePartExt{
				Disposition: &imap.BodyStructureDisposition{Value: "INLINE"},
			},
		},
	},
	{
		name: "message/global, IMAP4rev1 form, disposition with boundary parameter",
		data: `("MESSAGE" "GLOBAL" NIL NIL NIL "7BIT" 120 NIL ("ATTACHMENT" ("BOUNDARY" "x")) "EN")`,
		want: &imap.BodyStructureSinglePart{
			Type:     "MESSAGE",
			Subtype:  "GLOBAL",
			Encoding: "7BIT",
			Size:     120,
			Extended: &imap.BodyStructureSinglePartExt{
				Disposition: &imap.BodyStructureDisposition{
					Value:  "ATTACHMENT",
					Params: map[string]string{"boundary": "x"},
				},
				Language: []string{"EN"},
			},
		},
	},
	{
		name: "Dovecot, message/global",
		// https://github.com/emersion/go-imap/issues/678
		data: `(((("text" "plain" ("charset" "UTF-8") NIL NIL "quoted-printable" 576 31 NIL NIL NIL NIL)("text" "html" ("charset" "UTF-8") NIL NIL "quoted-printable" 4691 112 NIL NIL NIL NIL) "alternative" ("boundary" "----=_NextPart_002_0063_01D85E63.43189F50") NIL NIL NIL)("image" "png" ("name" "image001.png") "<image001.png@01D85E63.36D4F950>" NIL "base64" 3832 NIL NIL NIL NIL) "related" ("boundary" "----=_NextPart_001_0062_01D85E63.43189F50") NIL NIL NIL)("message" "delivery-status" ("name" "details.txt") NIL NIL "7bit" 594 NIL ("attachment" ("filename" "details.txt")) NIL NIL)("message" "global" ("name" "Untitled attachment 00019.dat") NIL NIL "7bit" 6726 NIL ("attachment" ("filename" "Untitled attachment 00019.dat")) NIL NIL) "mixed" ("boundary" "----=_NextPart_000_0061_01D85E63.43189F50") NIL ("en-us") NIL)`,
		want: &imap.BodyStructureMultiPart{
			Children: []imap.BodyStructure{
				&imap.BodyStructureMultiPart{
					Children: []imap.BodyStructure{
						&imap.BodyStructureMultiPart{
							Children: []imap.BodyStructure{
								&imap.BodyStructureSinglePart{
									Type:     "text",
									Subtype:  "plain",
									Params:   map[string]string{"charset": "UTF-8"},
									Encoding: "quoted-printable",
									Size:     576,
									Text:     &imap.BodyStructureText{NumLines: 31},
									Extended: &imap.BodyStructureSinglePartExt{},
								},
								&imap.BodyStructureSinglePart{
									Type:     "text",
									Subtype:  "html",
									Params:   map[string]string{"charset": "UTF-8"},
									Encoding: "quoted-printable",
									Size:     4691,
									Text:     &imap.BodyStructureText{NumLines: 112},
									Extended: &imap.BodyStructureSinglePartExt{},
								},
							},
							Subtype: "alternative",
							Extended: &imap.BodyStructureMultiPartExt{
								Params: map[string]string{"boundary": "----=_NextPart_002_0063_01D85E63.43189F50"},
							},
						},
						&imap.BodyStructureSinglePart{
							Type:     "image",
							Subtype:  "png",
							Params:   map[string]string{"name": "image001.png"},
							ID:       "<image001.png@01D85E63.36D4F950>",
							Encoding: "base64",
							Size:     3832,
							Extended: &imap.BodyStructureSinglePartExt{},
						},
					},
					Subtype: "related",
					Extended: &imap.BodyStructureMultiPartExt{
						Params: map[string]string{"boundary": "----=_NextPart_001_0062_01D85E63.43189F50"},
					},
				},
				&imap.BodyStructureSinglePart{
					Type:     "message",
					Subtype:  "delivery-status",
					Params:   map[string]string{"name": "details.txt"},
					Encoding: "7bit",
					Size:     594,
					Extended: &imap.BodyStructureSinglePartExt{
						Disposition: &imap.BodyStructureDisposition{
							Value:  "attachment",
							Params: map[string]string{"filename": "details.txt"},
						},
					},
				},
				&imap.BodyStructureSinglePart{
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
				},
			},
			Subtype: "mixed",
			Extended: &imap.BodyStructureMultiPartExt{
				Params:   map[string]string{"boundary": "----=_NextPart_000_0061_01D85E63.43189F50"},
				Language: []string{"en-us"},
			},
		},
	},
}

func newTestBodyDecoder(data string) *imapwire.Decoder {
	// Read one byte at a time, so that parsing doesn't depend on buffering
	r := iotest.OneByteReader(strings.NewReader(data + "\r\n"))
	return imapwire.NewDecoder(bufio.NewReader(r), imapwire.ConnSideClient)
}

func TestReadBody(t *testing.T) {
	for _, tc := range bodyStructureTests {
		t.Run(tc.name, func(t *testing.T) {
			dec := newTestBodyDecoder(tc.data)
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

func TestReadBody_invalid(t *testing.T) {
	const (
		msgNILEnvelope = `("MESSAGE" "RFC822" NIL NIL NIL "7BIT" 120 NIL `
		textPart       = `("TEXT" "PLAIN" NIL NIL NIL "7BIT" 10 1)`
		missingSubtype = `in body-type-1part: missing media subtype`
	)
	tests := []struct {
		name string
		data string
		// wantErr is a prefix of the error: the decoder only appends
		// ", got ..." when bytes are buffered
		wantErr string
	}{
		{
			name:    "NIL subtype",
			data:    `("APPLICATION" NIL NIL NIL NIL "BASE64" 100 NIL ("ATTACHMENT" ("FILENAME" "x")) NIL NIL)`,
			wantErr: `in body-type-1part: imapwire: expected string`,
		},
		{
			name:    "atom subtype",
			data:    `("TEXT" PLAIN NIL NIL NIL "7BIT" 10 1)`,
			wantErr: `in body-type-1part: imapwire: expected string`,
		},
		{
			name:    "message/rfc822, atom instead of envelope",
			data:    `("MESSAGE" "RFC822" NIL NIL NIL "7BIT" 120 FOO ("TEXT" "PLAIN" NIL NIL NIL "7BIT" 10 1) 4)`,
			wantErr: `in body-type-1part: imapwire: expected envelope or body-fld-md5, got "FOO"`,
		},
		{
			name:    "missing subtype",
			data:    `("TEXT" ("CHARSET" "UTF-8") NIL NIL "7BIT" 10 1)`,
			wantErr: missingSubtype,
		},
		{
			name:    "missing subtype, in multipart",
			data:    `(("TEXT" ("CHARSET" "UTF-8") NIL NIL "7BIT" 10 1)` + textPart + ` "MIXED")`,
			wantErr: `in body-type-mpart: ` + missingSubtype,
		},
		{
			name:    "missing subtype, NIL envelope",
			data:    msgNILEnvelope + `("TEXT" ("CHARSET" "UTF-8") NIL NIL "7BIT" 10 1) 4)`,
			wantErr: `in body-type-1part: ` + missingSubtype,
		},
		{
			name:    "missing subtype and parameters",
			data:    `("TEXT")`,
			wantErr: `in body-type-1part: imapwire: expected SP`,
		},
		{
			name:    "missing subtype and parameters, NIL envelope",
			data:    msgNILEnvelope + `("ALTERNATIVE") 4)`,
			wantErr: `in body-type-1part: in body-type-1part: imapwire: expected SP`,
		},
		{
			name:    "NIL subtype, NIL envelope",
			data:    msgNILEnvelope + `("APPLICATION" NIL NIL NIL "BASE64" 100 NIL NIL NIL NIL) 4)`,
			wantErr: `in body-type-1part: ` + missingSubtype,
		},
		{
			name:    "multipart with no children, NIL body-fld-param",
			data:    `("ALTERNATIVE" NIL NIL NIL)`,
			wantErr: `in body-type-1part: imapwire: expected string`,
		},
		{
			name:    "multipart with no children, NIL body-fld-param, NIL envelope",
			data:    msgNILEnvelope + `("ALTERNATIVE" NIL) 4)`,
			wantErr: `in body-type-1part: ` + missingSubtype,
		},
		{
			name:    "multipart with no children, no boundary",
			data:    `("ALTERNATIVE" ("CHARSET" "UTF-8") NIL NIL)`,
			wantErr: missingSubtype,
		},
		{
			name:    "multipart with no children, no boundary, NIL envelope",
			data:    msgNILEnvelope + `("ALTERNATIVE" ("CHARSET" "UTF-8")) 4)`,
			wantErr: `in body-type-1part: ` + missingSubtype,
		},
		{
			name:    "multipart with no children, top-level media type as subtype",
			data:    `("TEXT" ("BOUNDARY" "x"))`,
			wantErr: missingSubtype,
		},
		{
			name:    "multipart with no children, top-level media type as subtype, NIL envelope",
			data:    msgNILEnvelope + `("text" ("BOUNDARY" "x")) 4)`,
			wantErr: `in body-type-1part: ` + missingSubtype,
		},
		{
			name:    "multipart with no children, body-extension",
			data:    `("ALTERNATIVE" ("BOUNDARY" "x") NIL NIL NIL 1)`,
			wantErr: `in body-type-mpart: imapwire: expected ')'`,
		},
		{
			name:    "multipart with no children, key without value",
			data:    `(("ALTERNATIVE" ("BOUNDARY"))` + textPart + ` "MIXED")`,
			wantErr: `in body-type-mpart: in body-fld-param: key without value`,
		},
		{
			name:    "message/rfc822, NIL envelope and NIL body",
			data:    msgNILEnvelope + `NIL 4)`,
			wantErr: `in body-type-1part: in body-ext-1part: in body-fld-lang: imapwire: expected nstring`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readBody(newTestBodyDecoder(tc.data), &Options{})
			if err == nil || !strings.HasPrefix(err.Error(), tc.wantErr) {
				t.Errorf("readBody() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestReadBody_depth(t *testing.T) {
	const text = `("TEXT" "PLAIN" NIL NIL NIL "7BIT" 10 1)`
	nest := func(depth int, open, close string) string {
		return strings.Repeat(open, depth-1) + text + strings.Repeat(close, depth-1)
	}
	tests := []struct {
		name  string
		open  string
		close string
	}{
		{"multipart", "(", ` "MIXED")`},
		{"message/rfc822", `("MESSAGE" "RFC822" NIL NIL NIL "7BIT" 120 (NIL NIL NIL NIL NIL NIL NIL NIL NIL NIL) `, ` 4)`},
		{"message/rfc822, NIL envelope", `("MESSAGE" "RFC822" NIL NIL NIL "7BIT" 120 NIL `, ` 4)`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dec := newTestBodyDecoder(nest(maxBodyDepth, tc.open, tc.close))
			if _, err := readBody(dec, &Options{}); err != nil {
				t.Fatalf("readBody() at max depth = %v", err)
			}
			if !dec.ExpectCRLF() {
				t.Fatalf("ExpectCRLF() = %v", dec.Err())
			}

			_, err := readBody(newTestBodyDecoder(nest(maxBodyDepth+1, tc.open, tc.close)), &Options{})
			if err == nil || !strings.Contains(err.Error(), "body nested more than") {
				t.Errorf("readBody() past max depth = %v, want depth error", err)
			}
		})
	}

	t.Run("unterminated", func(t *testing.T) {
		_, err := readBody(newTestBodyDecoder(strings.Repeat("(", 10_000_000)), &Options{})
		if err == nil || !strings.Contains(err.Error(), "body nested more than") {
			t.Errorf("readBody() = %v, want depth error", err)
		}
	})
}

func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
