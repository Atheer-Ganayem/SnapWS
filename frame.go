package snapws

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
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

type Frame struct {
	FIN           bool
	OPCODE        uint8
	PayloadLength int
	IsMasked      bool
	MaskingKey    []byte
	Payload       []byte
}

type Message struct {
	OPCODE  uint8
	Payload *bytes.Buffer
}

func NewFrame(FIN bool, OPCODE uint8, IsMasked bool, payload []byte) (Frame, error) {
	frame := Frame{
		FIN:        FIN,
		OPCODE:     OPCODE,
		IsMasked:   IsMasked,
		Payload:    payload,
		MaskingKey: make([]byte, 4),
	}

	if payload != nil {
		frame.PayloadLength = len(payload)
	}

	if IsMasked {
		_, err := rand.Read(frame.MaskingKey)
		if err != nil {
			return frame, err
		}
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

func (frame *Frame) IsControl() bool {
	return frame.OPCODE == OpcodeClose || frame.OPCODE == OpcodePing || frame.OPCODE == OpcodePong
}

func (frame *Frame) IsValidControl() bool {
	return frame.FIN && frame.IsControl() && frame.PayloadLength < 126
}

func (message *Message) IsValidUTF8() bool {
	return utf8.Valid(message.Payload.Bytes())
}

func (message *Message) IsBinary() bool {
	return message.OPCODE == OpcodeBinary
}

func (message *Message) IsText() bool {
	return message.OPCODE == OpcodeText
}

func IsValidCloseCode(code uint16) bool {
	return slices.Contains(allowedCodes, code)
}

func ReadFrame(raw []byte) (Frame, error) {
	var frame Frame

	if len(raw) < 2 {
		return frame, errors.New("incomplete header (need at least 2 bytes)")
	}

	frame.FIN = raw[0]&0b10000000 != 0
	frame.OPCODE = raw[0] & 0b00001111
	frame.IsMasked = raw[1]&0b10000000 != 0
	rsv := raw[0] & 0b01110000
	if rsv != 0 {
		return frame, errors.New("non-zero reserved bits set without negotiated extension")
	}

	offset, err := frame.parsePayloadLength(raw)
	if err != nil {
		return frame, err
	}

	if frame.IsMasked {
		if len(raw) < offset+4 {
			return frame, errors.New("incomplete masking key")
		}
		frame.MaskingKey = raw[offset : offset+4]
		offset += 4
	}

	if len(raw) < offset+frame.PayloadLength {
		return frame, errors.New("incomplete payload")
	}

	maskedPayload := raw[offset : offset+frame.PayloadLength]
	frame.Payload = make([]byte, frame.PayloadLength)

	if frame.IsMasked {
		for i := 0; i < frame.PayloadLength; i++ {
			frame.Payload[i] = maskedPayload[i] ^ frame.MaskingKey[i%4]
		}
	} else {
		copy(frame.Payload, maskedPayload)
	}

	return frame, nil
}

func (frame *Frame) parsePayloadLength(raw []byte) (int, error) {
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
			return offset, fmt.Errorf("payload too large: %d", length64)
		}
		frame.PayloadLength = int(length64)
		offset += 8
	}

	return offset, nil
}

func (f *Frame) CalcLength() int {
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

func (f *Frame) Bytes() []byte {
	b := make([]byte, 2, f.CalcLength())

	b[0] = f.OPCODE
	if f.FIN {
		b[0] |= 0x80
	}

	if f.IsMasked {
		b[1] |= 0x80
	}

	if f.PayloadLength < 126 && f.PayloadLength > 0 {
		b[1] += byte(f.PayloadLength)
	} else if f.PayloadLength <= math.MaxUint16 {
		b[1] += 126
		b = binary.BigEndian.AppendUint16(b, uint16(f.PayloadLength))
	} else {
		b[1] += 127
		b = binary.BigEndian.AppendUint64(b, uint64(f.PayloadLength))
	}

	if f.IsMasked {
		b = append(b, f.MaskingKey...)

		masked := make([]byte, f.PayloadLength)
		copy(masked, f.Payload)
		mask(masked, f.MaskingKey)
		b = append(b, masked...)
	} else {
		b = append(b, f.Payload...)
	}

	return b
}

func mask(payload []byte, maskingKey []byte) {
	for i := range payload {
		payload[i] = payload[i] ^ maskingKey[i%4]
	}
}
