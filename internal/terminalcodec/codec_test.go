package terminalcodec

import (
	"bytes"
	"io"
	"testing"
	"testing/iotest"

	"sshclient/internal/connectcfg"
)

func TestLegacyTerminalEncodingAndBackspaceAcrossPacketBoundaries(t *testing.T) {
	for _, encoding := range []string{"gb18030", "gbk", ""} {
		t.Run(encoding, func(t *testing.T) {
			raw := []byte{'p', '>', 0xd6, 0xd0, 0xce, 0xc4, '\n'}
			var input bytes.Buffer
			output, keyboard, e := New(iotest.OneByteReader(bytes.NewReader(raw)), &input, connectcfg.Terminal{Encoding: encoding, Backspace: "bs"})
			if e != nil {
				t.Fatal(e)
			}
			defer output.Close()
			defer keyboard.Close()
			text, e := io.ReadAll(output)
			if e != nil || string(text) != "p>中文\n" {
				t.Fatalf("decode: %q %v", text, e)
			}
			for _, b := range []byte("中文\x7f\x08") {
				if _, e = keyboard.Write([]byte{b}); e != nil {
					t.Fatal(e)
				}
			}
			if !bytes.Equal(input.Bytes(), []byte{0xd6, 0xd0, 0xce, 0xc4, 8, 8}) {
				t.Fatalf("encoded terminal input: %x", input.Bytes())
			}
		})
	}
}
func TestGB18030FourByteCharactersAndUTF8Auto(t *testing.T) {
	for _, encoding := range []string{"gb18030", "utf-8", ""} {
		t.Run(encoding, func(t *testing.T) {
			text := []byte("中文😀\x1b[31m")
			var encoded bytes.Buffer
			_, writer, e := New(bytes.NewReader(nil), &encoded, connectcfg.Terminal{Encoding: encoding})
			if e != nil {
				t.Fatal(e)
			}
			for _, b := range text {
				if _, e = writer.Write([]byte{b}); e != nil {
					t.Fatal(e)
				}
			}
			writer.Close()
			reader, keyboard, e := New(iotest.OneByteReader(bytes.NewReader(encoded.Bytes())), io.Discard, connectcfg.Terminal{Encoding: encoding})
			if e != nil {
				t.Fatal(e)
			}
			defer reader.Close()
			defer keyboard.Close()
			decoded, e := io.ReadAll(reader)
			if e != nil || !bytes.Equal(decoded, text) {
				t.Fatalf("round trip: %q %v", decoded, e)
			}
		})
	}
}
func TestAutoEmitsASCIIPromptWithoutWaitingForMoreBytes(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	output, keyboard, e := New(r, io.Discard, connectcfg.Terminal{})
	if e != nil {
		t.Fatal(e)
	}
	defer output.Close()
	defer keyboard.Close()
	go w.Write([]byte{'>'})
	var b [1]byte
	if _, e = io.ReadFull(output, b[:]); e != nil || b[0] != '>' {
		t.Fatal(e)
	}
}

func TestGB18030SupplementaryMappingsMatchKnownBytes(t *testing.T) {
	for _, sample := range []struct {
		text    string
		encoded []byte
	}{{"\U00010000", []byte{0x90, 0x30, 0x81, 0x30}}, {"😀", []byte{0x94, 0x39, 0xfc, 0x36}}, {"\U0010ffff", []byte{0xe3, 0x32, 0x9a, 0x35}}} {
		var encoded bytes.Buffer
		reader, writer, e := New(bytes.NewReader(sample.encoded), &encoded, connectcfg.Terminal{Encoding: "gb18030"})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = writer.Write([]byte(sample.text)); e != nil {
			t.Fatal(e)
		}
		actual, e := io.ReadAll(reader)
		reader.Close()
		writer.Close()
		if e != nil || string(actual) != sample.text || !bytes.Equal(encoded.Bytes(), sample.encoded) {
			t.Fatalf("supplementary mapping: %x %q %v", encoded.Bytes(), actual, e)
		}
	}
}

func TestDefaultBackspaceDoesNotRewriteExplicitControlH(t *testing.T) {
	var input bytes.Buffer
	reader, keyboard, e := New(bytes.NewReader(nil), &input, connectcfg.Terminal{})
	if e != nil {
		t.Fatal(e)
	}
	defer reader.Close()
	defer keyboard.Close()
	if _, e = keyboard.Write([]byte{8, 127}); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(input.Bytes(), []byte{8, 127}) {
		t.Fatal("default backspace mapping changed an explicit Ctrl+H shortcut")
	}
}
