package snapws

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewFrame_Unmasked(t *testing.T) {
	payload := []byte("hello")
	frame, err := NewFrame(true, OpcodeText, false, payload)
	assert.NoError(t, err)
	assert.Equal(t, true, frame.FIN)
	assert.Equal(t, uint8(OpcodeText), frame.OPCODE)
	assert.Equal(t, false, frame.IsMasked)
	assert.Equal(t, payload, frame.Payload)
	assert.Equal(t, len(payload), frame.PayloadLength)
}

func TestIsCompleteFrame_ShortData(t *testing.T) {
	ok, err := IsCompleteFrame([]byte{0x81})
	assert.NoError(t, err)
	assert.False(t, ok)
}

func TestIsCompleteFrame_FullSmallFrame(t *testing.T) {
	// FIN + Text frame + Masked + 5-byte payload
	header := []byte{0x81, 0x85}
	mask := []byte{0x01, 0x02, 0x03, 0x04}
	payload := []byte("hello")
	masked := make([]byte, len(payload))
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	frame := append(append(header, mask...), masked...)
	ok, err := IsCompleteFrame(frame)
	assert.NoError(t, err)
	assert.True(t, ok)
}

func TestIsValidCloseCode(t *testing.T) {
	assert.True(t, IsValidCloseCode(1000))
	assert.False(t, IsValidCloseCode(999))
}

func TestReadFrame_UnmaskedText(t *testing.T) {
	payload := []byte("hi")
	header := []byte{0x81, byte(len(payload))} // FIN + Text, No mask
	frameBytes := append(header, payload...)

	frame, err := ReadFrame(frameBytes)
	assert.NoError(t, err)
	assert.Equal(t, payload, frame.Payload)
	assert.Equal(t, uint8(OpcodeText), frame.OPCODE)
	assert.False(t, frame.IsMasked)
}

func TestReadFrame_MaskedText(t *testing.T) {
	payload := []byte("hi")
	mask := []byte{0xaa, 0xbb, 0xcc, 0xdd}
	maskedPayload := []byte{payload[0] ^ mask[0], payload[1] ^ mask[1]}
	header := []byte{0x81, 0x80 | byte(len(payload))}
	frameBytes := append(append(header, mask...), maskedPayload...)

	frame, err := ReadFrame(frameBytes)
	assert.NoError(t, err)
	assert.Equal(t, payload, frame.Payload)
	assert.True(t, frame.IsMasked)
	assert.Equal(t, mask, frame.MaskingKey)
}

func TestReadFrame_IncompleteHeader(t *testing.T) {
	_, err := ReadFrame([]byte{0x81})
	assert.Error(t, err)
}

func TestReadFrame_InvalidRSV(t *testing.T) {
	data := []byte{0x70, 0x80} // RSV bits set
	_, err := ReadFrame(data)
	assert.Error(t, err)
}

func TestReadFrame_ExtendedPayload(t *testing.T) {
	// 126 payload length => next 2 bytes are length
	payload := make([]byte, 130)
	header := []byte{0x82, 126}
	lenBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(lenBytes, uint16(len(payload)))
	frameBytes := append(append(header, lenBytes...), payload...)

	frame, err := ReadFrame(frameBytes)
	assert.NoError(t, err)
	assert.Equal(t, len(payload), frame.PayloadLength)
}

func TestReadFrame_MaskingKeyTooShort(t *testing.T) {
	data := []byte{0x81, 0x80, 0x00, 0x00} // Not enough for masking key
	_, err := ReadFrame(data)
	assert.Error(t, err)
}

func TestReadFrame_PayloadTruncated(t *testing.T) {
	payload := []byte("xyz")
	masked := []byte{payload[0] ^ 0x01, payload[1] ^ 0x02, payload[2] ^ 0x03}
	frame := append([]byte{0x81, 0x83}, 0x01, 0x02, 0x03, 0x04) // Mask + 3 bytes payload
	frame = append(frame, masked[:2]...)                        // One byte short
	_, err := ReadFrame(frame)
	assert.Error(t, err)
}

func TestReadFrame_ExtendedPayload64(t *testing.T) {
	payload := make([]byte, 300)
	header := []byte{0x82, 127}
	lenBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(lenBytes, uint64(len(payload)))
	frameBytes := append(append(header, lenBytes...), payload...)

	frame, err := ReadFrame(frameBytes)
	assert.NoError(t, err)
	assert.Equal(t, len(payload), frame.PayloadLength)
}
