package snapws

import (
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
)

const MaxHeaderSize = 14
const MaxControlFramePayload = 125

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
