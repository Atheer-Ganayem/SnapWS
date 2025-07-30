package snapws

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math"
	"slices"
	"unicode/utf8"
)

const (
	CloseNormalClosure           uint16 = 1000
	CloseGoingAway               uint16 = 1001
	CloseProtocolError           uint16 = 1002
	CloseUnsupportedData         uint16 = 1003
	CloseInvalidFramePayloadData uint16 = 1007
	ClosePolicyViolation         uint16 = 1008
	CloseMessageTooBig           uint16 = 1009
	CloseMandatoryExtension      uint16 = 1010
	CloseInternalServerErr       uint16 = 1011
)

var allowedCodes = []uint16{1000, 1001, 1002, 1003, 1007, 1008, 1009, 1010, 1011}

const (
	OpcodeContinuation = 0x0 // Continuation frame
	OpcodeText         = 0x1 // Text frame (UTF-8)
	OpcodeBinary       = 0x2 // Binary frame
	OpcodeClose        = 0x8 // Connection close
	OpcodePing         = 0x9 // Ping
	OpcodePong         = 0xA // Pong
)

const MaxHeaderSize = 14

type frame struct {
	FIN           bool
	OPCODE        uint8
	PayloadLength int
	IsMasked      bool
	// not garantueed to be masked.
	Encoded       []byte
	PayloadOffset int
}

type message struct {
	OPCODE  uint8
	Payload *bytes.Buffer
}

func newFrame(FIN bool, OPCODE uint8, isMasked bool, payload []byte) (frame, error) {
	frame := frame{
		FIN:      FIN,
		OPCODE:   OPCODE,
		IsMasked: isMasked,
	}

	if payload != nil {
		frame.PayloadLength = len(payload)
	}
	err := frame.encode()
	frame.Encoded = append(frame.Encoded, payload...)

	return frame, err
}

// Reads a slice of bytes into a frame and returns the pointer.
// If the frame is masked, it will unmask it.
// Note: the payload is copied, not refrenced to the slice.
func readFrame(raw []byte) (*frame, error) {
	if len(raw) < 2 {
		return nil, errors.New("-incomplete header (need at least 2 bytes)")
	}

	frame := &frame{}
	frame.FIN = raw[0]&0b10000000 != 0
	frame.OPCODE = raw[0] & 0b00001111
	frame.IsMasked = raw[1]&0b10000000 != 0
	rsv := raw[0] & 0b01110000
	if !frame.isValidOpcode() {
		return nil, ErrInvalidOPCODE
	} else if rsv != 0 {
		return nil, errors.New("non-zero reserved bits set without negotiated extension")
	}

	offset, err := frame.parsePayloadLength(raw)
	if err != nil {
		return nil, err
	}

	var maskingKey []byte
	if frame.IsMasked {
		if len(raw) < offset+4 {
			return nil, errors.New("incomplete masking key")
		}
		maskingKey = raw[offset : offset+4]
		offset += 4
	}

	if len(raw) < offset+frame.PayloadLength {
		return nil, errors.New("incomplete payload")
	}

	if frame.IsMasked {
		for i := range raw[offset : offset+frame.PayloadLength] {
			raw[offset+i] = raw[offset+i] ^ maskingKey[i%4]
		}
	}

	frame.PayloadOffset = offset

	frame.Encoded = make([]byte, len(raw))
	copy(frame.Encoded, raw)

	return frame, nil
}

// Asigns frame.PayloadLength to the calculated length.
// Returns int, a value of the offset where the "length bytes" end.
// Also returns an error of a confilict happened (eg: bytes less than 2, length byte is 126
// but the total bytes are less than 4, etc...)
func (frame *frame) parsePayloadLength(raw []byte) (int, error) {
	if len(raw) < 2 {
		return -1, errors.New("--incomplete header (need at least 2 bytes)")
	}

	payloadLen := int(raw[1] & 0b01111111)
	offset := 2

	if payloadLen < 126 {
		frame.PayloadLength = payloadLen
	} else if payloadLen == 126 {
		if len(raw) < offset+2 {
			return offset, errors.New("incomplete extended 16-bit length")
		}
		frame.PayloadLength = int(binary.BigEndian.Uint16(raw[offset : offset+2]))
		offset += 2
	} else if payloadLen == 127 {
		if len(raw) < offset+8 {
			return offset, errors.New("incomplete extended 64-bit length")
		}
		length64 := binary.BigEndian.Uint64(raw[offset : offset+8])
		if length64 > math.MaxInt32 {
			return offset, ErrTooLargePayload
		}
		frame.PayloadLength = int(length64)
		offset += 8
	}

	return offset, nil
}

func (f *frame) calcLength() int {
	length := 2 + f.PayloadLength

	if f.IsMasked {
		length += 4
	}

	switch {
	case f.PayloadLength < 126:
		// no extended length bytres
	case f.PayloadLength <= math.MaxUint16:
		length += 2
	default:
		length += 8
	}

	return length
}

func (f *frame) encode() error {
	length := f.calcLength()
	if cap(f.Encoded) < length {
		f.Encoded = make([]byte, 2, length)
	}

	f.PayloadOffset = 2

	f.Encoded[0] = f.OPCODE
	if f.FIN {
		f.Encoded[0] |= 0x80
	}

	if f.IsMasked {
		f.Encoded[1] |= 0x80
	}

	if f.PayloadLength < 126 && f.PayloadLength >= 0 {
		f.Encoded[1] += byte(f.PayloadLength)
	} else if f.PayloadLength <= math.MaxUint16 {
		f.Encoded[1] += 126
		f.Encoded = binary.BigEndian.AppendUint16(f.Encoded, uint16(f.PayloadLength))
		f.PayloadOffset += 2
	} else {
		f.Encoded[1] += 127
		f.Encoded = binary.BigEndian.AppendUint64(f.Encoded, uint64(f.PayloadLength))
		f.PayloadOffset += 8
	}

	if f.IsMasked {
		f.Encoded = f.Encoded[:len(f.Encoded)+4]
		_, err := rand.Read(f.Encoded[len(f.Encoded)-4:])
		if err != nil {
			return err
		}
		f.PayloadOffset += 4
	}

	return nil
}

func (f *frame) mask() {
	if !f.IsMasked {
		return
	}

	maskingKey := f.Encoded[f.PayloadOffset-4 : f.PayloadOffset]
	for i := range f.Encoded[f.PayloadOffset : f.PayloadOffset+f.PayloadLength] {
		f.Encoded[f.PayloadOffset+i] = f.Encoded[f.PayloadOffset+i] ^ maskingKey[i%4]
	}
}

// return the total expected length of the frame.
// if it is not possible to expect the expected length out of the given byte slice,
// it return a non-nil error
func readLength(b []byte) (int, error) {
	if len(b) < 2 {
		return -1, errors.New("incomplete header (need at least 2 bytes)")
	}

	isMasked := b[1]&0b10000000 != 0
	payloadLen := int(b[1] & 0b01111111)
	if payloadLen < 0 {
		return -1, fatal(ErrInvalidPayloadLength)
	}

	total := 2
	if isMasked {
		total += 4
	}

	if payloadLen < 126 {
		total += payloadLen
	} else if payloadLen == 126 {
		if len(b) < 4 {
			return -1, errors.New("incomplete extended 16-bit length")
		}
		total += int(binary.BigEndian.Uint16(b[2:4])) + 2
	} else if payloadLen == 127 {
		if len(b) < 10 {
			return -1, errors.New("incomplete extended 64-bit length")
		}
		length64 := binary.BigEndian.Uint64(b[2:10])
		if length64 > math.MaxInt32 {
			return -1, fatal(ErrTooLargePayload)
		}
		total += int(length64) + 8
	}

	return total, nil
}

// Write the frame header to a ConnWriter, must have at least 14 empty bytes at the begining.
// Note: it doesnt supporting writing masking key, will support later.
func (w *ConnWriter) writeHeaders(FIN bool, OPCODE uint8) error {
	if w.start < 14 {
		return ErrInsufficientHeaderSpace
	}

	payloadLength := w.used - w.start

	if payloadLength < 0 {
		return ErrInvalidPayloadLength
	} else if payloadLength < 126 {
		w.buf[w.start-1] = byte(payloadLength)
		w.start--
	} else if payloadLength <= math.MaxUint16 {
		binary.BigEndian.PutUint16(w.buf[w.start-2:w.start], uint16(payloadLength))
		w.buf[w.start-3] = 126
		w.start -= 3
	} else {
		binary.BigEndian.PutUint64(w.buf[w.start-8:w.start], uint64(payloadLength))
		w.buf[w.start-9] = 127
		w.start -= 9
	}

	w.buf[w.start-1] = OPCODE
	if FIN {
		w.buf[w.start-1] |= 0x80
	}
	w.start--

	return nil
}

func (frame *frame) payload() []byte {
	return frame.Encoded[frame.PayloadOffset : frame.PayloadOffset+frame.PayloadLength]
}

func (frame *frame) isValidOpcode() bool {
	return frame.OPCODE == OpcodeBinary || frame.isText() || frame.isControl()
}

func (frame *frame) isText() bool {
	return frame.OPCODE == OpcodeText
}

func (frame *frame) isControl() bool {
	return frame.OPCODE == OpcodeClose || frame.OPCODE == OpcodePing || frame.OPCODE == OpcodePong
}

func (frame *frame) isValidControl() bool {
	return frame.FIN && frame.isControl() && frame.PayloadLength < 126
}

func (message *message) isValidUTF8() bool {
	return utf8.Valid(message.Payload.Bytes())
}

func (message *message) isText() bool {
	return message.OPCODE == OpcodeText
}

func isValidCloseCode(code uint16) bool {
	return slices.Contains(allowedCodes, code)
}
