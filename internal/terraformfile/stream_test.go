package terraformfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"
	"time"
)

// endlessStream is a stream that never ends: every Read fills p with spaces.
// It counts the bytes it hands out.
type endlessStream struct{ given int64 }

func (e *endlessStream) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	e.given += int64(len(p))
	return len(p), nil
}

// streamOf hides a reader's Len, as standard input does.
type streamOf struct{ r io.Reader }

func (s streamOf) Read(p []byte) (int, error) { return s.r.Read(p) }

type readResult struct {
	acquisition *Acquisition
	refusal     *Refusal
	err         error
}

// readStreamWithin runs ReadStream and fails if it has not returned in 5 s.
func readStreamWithin(t *testing.T, r io.Reader, src Source) (*Acquisition, *Refusal) {
	t.Helper()
	done := make(chan readResult, 1)
	go func() {
		acquisition, refusal, err := ReadStream(r, src)
		done <- readResult{acquisition, refusal, err}
	}()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("ReadStream: %v", got.err)
		}
		return got.acquisition, got.refusal
	case <-time.After(5 * time.Second):
		t.Fatal("ReadStream has not returned within 5 s")
		return nil, nil
	}
}

// checkStreamSizeRefusal holds a stream's TF-SIZE to its code and fixed
// message alone.
func checkStreamSizeRefusal(t *testing.T, refusal *Refusal) {
	t.Helper()
	if refusal == nil {
		t.Fatal("accepted, want TF-SIZE")
	}
	if refusal.Code != CodeSize || !refusal.Stream || refusal.SHA256 != "" || refusal.Bytes != 0 || refusal.Message != streamSizeMessage {
		t.Fatalf("stream refusal %+v", refusal)
	}
	want := `{"code":"TF-SIZE","message":` + string(encodeJSON(t, streamSizeMessage)) + `}`
	if string(refusal.JSON()) != want {
		t.Fatalf("stream refusal JSON\n got %s\nwant %s", refusal.JSON(), want)
	}
}

func encodeJSON(t *testing.T, value string) []byte {
	t.Helper()
	return appendJSONString(nil, value)
}

func TestAnEndlessStreamIsRefusedOneBytePastMaxBytes(t *testing.T) {
	for _, maxBytes := range []int64{1, 1 << 20, 0} {
		src := planSource("synthetic_db")
		src.MaxBytes = maxBytes
		stream := &endlessStream{}
		_, refusal := readStreamWithin(t, stream, src)
		checkStreamSizeRefusal(t, refusal)
		if want := src.maxBytes() + 1; stream.given != want {
			t.Fatalf("maxBytes %d: read %d bytes of the stream, want %d", src.maxBytes(), stream.given, want)
		}
	}
}

func TestAStreamOfExactlyMaxBytesIsReadWhole(t *testing.T) {
	data := plan(t, change{address: "synthetic_db.main", after: object("size", 1)})
	src := planSource("synthetic_db")
	src.MaxBytes = int64(len(data))
	acquisition, refusal := readStreamWithin(t, streamOf{bytes.NewReader(data)}, src)
	if refusal != nil {
		t.Fatalf("refused %s at maxBytes", refusal.Code)
	}
	sum := sha256.Sum256(data)
	if acquisition.SHA256 != hex.EncodeToString(sum[:]) || acquisition.Bytes != int64(len(data)) || acquisition.Selected != 1 {
		t.Fatalf("accepted with %s/%d/%d", acquisition.SHA256, acquisition.Bytes, acquisition.Selected)
	}
	longer := append(append([]byte(nil), data...), ' ')
	_, refusal = readStreamWithin(t, streamOf{bytes.NewReader(longer)}, src)
	checkStreamSizeRefusal(t, refusal)
	// A reader that tells its size is still a stream when the caller says so.
	_, refusal = readStreamWithin(t, bytes.NewReader(longer), src)
	checkStreamSizeRefusal(t, refusal)
	// Read of the same bytes is a file's TF-SIZE: the whole digest and size.
	refusal = mustRefuse(t, longer, src, CodeSize)
	if refusal.Stream {
		t.Fatal("Read's TF-SIZE is marked as a stream's")
	}
}

func TestAStreamWithinMaxBytesRefusesWithItsDigestAndSize(t *testing.T) {
	data := []byte(`{"format_version":"1.2"`)
	_, refusal := readStreamWithin(t, streamOf{bytes.NewReader(data)}, planSource("synthetic_db"))
	if refusal == nil || refusal.Code != CodeJSON || refusal.Stream {
		t.Fatalf("refusal %+v, want a TF-JSON that is not a stream's TF-SIZE", refusal)
	}
	checkRefusal(t, data, refusal)
}
