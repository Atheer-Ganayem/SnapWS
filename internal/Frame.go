package internal

import "crypto/rand"

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
