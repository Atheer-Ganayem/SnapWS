package internal

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math"
)

const (
	OpcodeContinuation = 0x0 // Continuation frame
	OpcodeText         = 0x1 // Text frame (UTF-8)
	OpcodeBinary       = 0x2 // Binary frame
	OpcodeClose        = 0x8 // Connection close
	OpcodePing         = 0x9 // Ping
	OpcodePong         = 0xA // Pong
)

type Frame struct {
	FIN           bool
	OPCODE        uint8
	PayloadLength int
	IsMasked      bool
	MaskingKey    []byte
	Payload       []byte
}

func NewFrame(FIN bool, OPCODE uint8, IsMasked bool, payload []byte) (Frame, error) {
	frame := Frame{
		FIN:        FIN,
		OPCODE:     OPCODE,
		IsMasked:   IsMasked,
		Payload:    payload,
		MaskingKey: make([]byte, 4),
	}
	frame.PayloadLength = len(payload)

	_, err := rand.Read(frame.MaskingKey)
	if err != nil {
		return frame, err
	}

	return frame, nil
}

func (frame *Frame) IsText() bool {
	return frame.OPCODE == OpcodeText
}
func IsCompleteFrame(raw []byte) (bool, error) {
	if len(raw) < 2 {
		return false, nil
	}

	isMasked := raw[1]&0b10000000 != 0
	payloadLen := int(raw[1] & 0b01111111)
	offset := 2

	if payloadLen == 126 {
		if len(raw) < offset+2 {
			return false, nil
		}
		payloadLen = int(binary.BigEndian.Uint16(raw[offset : offset+2]))
		offset += 2
	} else if payloadLen == 127 {
		if len(raw) < offset+8 {
			return false, nil
		}
		int64Len := binary.BigEndian.Uint64(raw[offset : offset+8])
		if int64Len > math.MaxInt {
			return false, fmt.Errorf("payload length too large: %d", int64Len)
		}
		payloadLen = int(int64Len)
		offset += 8
	}

	if isMasked {
		if len(raw) < offset+4 {
			return false, nil
		}
		offset += 4
	}

	if len(raw) < offset+payloadLen {
		return false, nil
	}

	return true, nil
}
