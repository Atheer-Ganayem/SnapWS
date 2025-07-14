package internal

import (
	"encoding/binary"
	"math"
)

func (f *Frame) CalcLength() int {
	length := 2 + f.PayloadLength

	if f.IsMasked {
		length += 4
	}

	switch {
	case f.PayloadLength < 126:
	case f.PayloadLength <= math.MaxUint16:
		length += 2
	default:
		length += 8
	}

	return length
}

func (f *Frame) Bytes() []byte {
	bytes := make([]byte, 2)

	bytes[0] = f.OPCODE
	if f.FIN {
		bytes[0] += 128
	}

	if f.IsMasked {
		bytes[1] = 128
	}

	if f.PayloadLength < 126 {
		bytes[1] += byte(f.PayloadLength)
	} else if f.PayloadLength <= math.MaxUint16 {
		bytes[1] += 126
		bytes = binary.BigEndian.AppendUint16(bytes, uint16(f.PayloadLength))
	} else {
		bytes[1] += 127
		bytes = binary.BigEndian.AppendUint64(bytes, uint64(f.PayloadLength))
	}

	if f.IsMasked {
		bytes = append(bytes, f.MaskingKey...)

		masked := make([]byte, f.PayloadLength)
		copy(masked, f.Payload)
		mask(masked, f.MaskingKey)
		bytes = append(bytes, masked...)
	} else {
		bytes = append(bytes, f.Payload...)
	}

	return bytes
}

func mask(payload []byte, maskingKey []byte) {
	for i, _ := range payload {
		payload[i] = payload[i] ^ maskingKey[i%4]
	}
}
