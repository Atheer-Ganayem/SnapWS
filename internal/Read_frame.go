package internal

import (
	"encoding/binary"
	"errors"
)

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
		frame.PayloadLength = int(binary.BigEndian.Uint64(raw[offset : offset+8]))
		offset += 8
	}

	return offset, nil
}
