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
		// no extended length bytres
	case f.PayloadLength <= math.MaxUint16:
		length += 2
	default:
		length += 8
	}

	return length
}

func (f *Frame) Bytes() []byte {
	b := make([]byte, 0, f.CalcLength())

	b[0] = f.OPCODE
	if f.FIN {
		b[0] |= 0x80
	}

	if f.IsMasked {
		b[1] |= 0x80
	}

	if f.PayloadLength < 126 {
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
