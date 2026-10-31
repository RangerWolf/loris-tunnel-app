package cloudsync

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"testing"
)

func TestWholeFileRoundTripAndValidation(t *testing.T) {
	data := []byte("# preserved comment\nversion=1\n[license]\ncode='license-secret'\n[[jumpers]]\nid=1\npassword='password-secret'\nkey_path='C:\\keys\\ssh'\nsync_id='legacy'\n")
	c, e := Pack(data)
	if e != nil {
		t.Fatal(e)
	}
	got, e := c.Unpack()
	if e != nil || !bytes.Equal(data, got) {
		t.Fatal("raw file round trip", e)
	}
	packed, _ := base64.StdEncoding.DecodeString(c.Data)
	packed[len(packed)-1] ^= 1
	c.Data = base64.StdEncoding.EncodeToString(packed)
	if _, e = c.Unpack(); e == nil {
		t.Fatal("corrupt gzip accepted")
	}
	if _, e = Pack(bytes.Repeat([]byte("x"), MaxFileSize+1)); e == nil {
		t.Fatal("oversize accepted")
	}
	if _, e = Pack([]byte("broken = [")); e == nil {
		t.Fatal("invalid TOML accepted")
	}
}
func TestBoundedDecompressionAndInvalidTOML(t *testing.T) {
	for _, data := range [][]byte{bytes.Repeat([]byte("x"), MaxFileSize+1), []byte("bad = [")} {
		var buf bytes.Buffer
		z := gzip.NewWriter(&buf)
		z.Write(data)
		z.Close()
		size := len(data)
		if size > MaxFileSize {
			size = 1
		}
		c := Config{SchemaVersion, base64.StdEncoding.EncodeToString(buf.Bytes()), size}
		if _, e := c.Unpack(); e == nil {
			t.Fatal("invalid decompressed file accepted")
		}
	}
}
