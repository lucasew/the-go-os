// Package control implements framed protobuf RPC over a Unix socket.
package control

import (
	"encoding/binary"
	"fmt"
	"io"
)

// MaxMessage is the max framed payload size (4 MiB).
const MaxMessage = 4 << 20

// WriteFrame writes a length-prefixed message (uint32 big-endian + bytes).
func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) > MaxMessage {
		return fmt.Errorf("control: message too large: %d", len(payload))
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// ReadFrame reads one length-prefixed message.
func ReadFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > MaxMessage {
		return nil, fmt.Errorf("control: message too large: %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
