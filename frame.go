package snapws

import (
	"crypto/rand"
	"encoding/binary"
	"math"
	"slices"
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

	// internal use, used when a function return an opcode but an error occured.
	// In other words: default value of an opcode. because the default value of uint8 is 0,
	// and 0 indicates "OpcodeText".
	nilOpcode = 0xff
)

const MaxHeaderSize = 14
const MaxControlFramePayload = 125

// writeHeaders method is a convenience method.
//
// It calls writeHeaders function with the correct arguments extracted from the ConnWriter.
func (w *ConnWriter) writeHeaders(FIN bool, OPCODE uint8) error {
	newStart, err := writeHeaders(w.buf, w.start, w.used, FIN, OPCODE, !w.conn.isServer)
	w.start = newStart
	return err
}

// writeHeaders takes a slice of bytes that represnts a frame.
//
// The slice must have at least it's first 14 bytes empty (start >= 14), these 14 >= bytes will be used
// to write the frame's header (fin, opcode, length, masking key, etc...).
//
// Returns an int representing the new start value, and an error.
func writeHeaders(buf []byte, start, used int, FIN bool, OPCODE uint8, maskPayload bool) (int, error) {
	if start < 14 {
		return start, ErrInsufficientHeaderSpace
	}

	if maskPayload {
		_, err := rand.Read(buf[start-4 : start])
		if err != nil {
			return start, err
		}
		mask(buf[start:used], buf[start-4:start], 0)
		start -= 4
	}

	payloadLength := used - start

	if payloadLength < 0 {
		return start, ErrInvalidPayloadLength
	} else if payloadLength < 126 {
		buf[start-1] = byte(payloadLength)
		start--
	} else if payloadLength <= math.MaxUint16 {
		binary.BigEndian.PutUint16(buf[start-2:start], uint16(payloadLength))
		buf[start-3] = 126
		start -= 3
	} else {
		binary.BigEndian.PutUint64(buf[start-8:start], uint64(payloadLength))
		buf[start-9] = 127
		start -= 9
	}

	buf[start-1] = OPCODE
	if FIN {
		buf[start-1] |= 0x80
	}
	start--

	return start, nil
}

func isValidCloseCode(code uint16) bool {
	if slices.Contains(allowedCodes, code) {
		return true
	}

	// private close codes
	if code >= 3000 && code <= 4999 {
		return true
	}

	return false
}

func isVaidOpcode(opcode byte) bool {
	return isControl(opcode) || isData(opcode) || opcode == OpcodeContinuation
}

func isControl(opcode byte) bool {
	return opcode == OpcodeClose || opcode == OpcodePing || opcode == OpcodePong
}

func isData(opcode byte) bool {
	return opcode == OpcodeText || opcode == OpcodeBinary
}
