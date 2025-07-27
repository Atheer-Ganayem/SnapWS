package snapws

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestNewFrame(t *testing.T) {
	tests := []struct {
		name      string
		FIN       bool
		OPCODE    uint8
		isMasked  bool
		payload   []byte
		wantError bool
	}{
		{
			name:     "simple text frame",
			FIN:      true,
			OPCODE:   OpcodeText,
			isMasked: false,
			payload:  []byte("hello"),
		},
		{
			name:     "binary frame with mask",
			FIN:      true,
			OPCODE:   OpcodeBinary,
			isMasked: true,
			payload:  []byte{0x01, 0x02, 0x03},
		},
		{
			name:     "ping frame no payload",
			FIN:      true,
			OPCODE:   OpcodePing,
			isMasked: false,
			payload:  nil,
		},
		{
			name:     "continuation frame",
			FIN:      false,
			OPCODE:   OpcodeContinuation,
			isMasked: false,
			payload:  []byte("partial"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame, err := newFrame(tt.FIN, tt.OPCODE, tt.isMasked, tt.payload)

			if tt.wantError && err == nil {
				t.Error("expected error but got none")
				return
			}
			if !tt.wantError && err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if frame.FIN != tt.FIN {
				t.Errorf("FIN = %v, want %v", frame.FIN, tt.FIN)
			}
			if frame.OPCODE != tt.OPCODE {
				t.Errorf("OPCODE = %v, want %v", frame.OPCODE, tt.OPCODE)
			}
			if frame.IsMasked != tt.isMasked {
				t.Errorf("IsMasked = %v, want %v", frame.IsMasked, tt.isMasked)
			}

			expectedLen := 0
			if tt.payload != nil {
				expectedLen = len(tt.payload)
			}
			if frame.PayloadLength != expectedLen {
				t.Errorf("PayloadLength = %v, want %v", frame.PayloadLength, expectedLen)
			}
		})
	}
}

func TestReadFrame(t *testing.T) {
	tests := []struct {
		name      string
		raw       []byte
		want      *frame
		wantError bool
	}{
		{
			name: "simple unmasked text frame",
			raw:  []byte{0x81, 0x05, 'h', 'e', 'l', 'l', 'o'}, // FIN=1, OPCODE=1, len=5
			want: &frame{
				FIN:           true,
				OPCODE:        OpcodeText,
				PayloadLength: 5,
				IsMasked:      false,
				PayloadOffset: 2,
			},
		},
		{
			name: "masked binary frame",
			raw:  []byte{0x82, 0x83, 0x12, 0x34, 0x56, 0x78, 0x01 ^ 0x12, 0x02 ^ 0x34, 0x03 ^ 0x56}, // FIN=1, OPCODE=2, MASK=1, len=3
			want: &frame{
				FIN:           true,
				OPCODE:        OpcodeBinary,
				PayloadLength: 3,
				IsMasked:      true,
				PayloadOffset: 6,
			},
		},
		{
			name: "ping frame",
			raw:  []byte{0x89, 0x00}, // FIN=1, OPCODE=9, len=0
			want: &frame{
				FIN:           true,
				OPCODE:        OpcodePing,
				PayloadLength: 0,
				IsMasked:      false,
				PayloadOffset: 2,
			},
		},
		{
			name:      "incomplete header",
			raw:       []byte{0x81},
			wantError: true,
		},
		{
			name:      "invalid opcode",
			raw:       []byte{0x8F, 0x00}, // FIN=1, OPCODE=15 (invalid)
			wantError: true,
		},
		{
			name:      "non-zero RSV bits",
			raw:       []byte{0xC1, 0x00}, // FIN=1, RSV=011, OPCODE=1
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame, err := readFrame(tt.raw)

			if tt.wantError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if frame.FIN != tt.want.FIN {
				t.Errorf("FIN = %v, want %v", frame.FIN, tt.want.FIN)
			}
			if frame.OPCODE != tt.want.OPCODE {
				t.Errorf("OPCODE = %v, want %v", frame.OPCODE, tt.want.OPCODE)
			}
			if frame.PayloadLength != tt.want.PayloadLength {
				t.Errorf("PayloadLength = %v, want %v", frame.PayloadLength, tt.want.PayloadLength)
			}
			if frame.IsMasked != tt.want.IsMasked {
				t.Errorf("IsMasked = %v, want %v", frame.IsMasked, tt.want.IsMasked)
			}
			if frame.PayloadOffset != tt.want.PayloadOffset {
				t.Errorf("PayloadOffset = %v, want %v", frame.PayloadOffset, tt.want.PayloadOffset)
			}
		})
	}
}

func TestParsePayloadLength(t *testing.T) {
	tests := []struct {
		name       string
		raw        []byte
		wantLength int
		wantOffset int
		wantError  bool
	}{
		{
			name:       "small payload (< 126)",
			raw:        []byte{0x81, 0x05},
			wantLength: 5,
			wantOffset: 2,
		},
		{
			name:       "medium payload (126)",
			raw:        []byte{0x81, 0x7E, 0x01, 0x00}, // 256 bytes
			wantLength: 256,
			wantOffset: 4,
		},
		{
			name:       "large payload (127)",
			raw:        []byte{0x81, 0x7F, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00}, // 65536 bytes
			wantLength: 65536,
			wantOffset: 10,
		},
		{
			name:      "incomplete header",
			raw:       []byte{0x81},
			wantError: true,
		},
		{
			name:      "incomplete 16-bit length",
			raw:       []byte{0x81, 0x7E, 0x01},
			wantError: true,
		},
		{
			name:      "incomplete 64-bit length",
			raw:       []byte{0x81, 0x7F, 0x00, 0x00, 0x00},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := &frame{}
			offset, err := frame.parsePayloadLength(tt.raw)

			if tt.wantError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if frame.PayloadLength != tt.wantLength {
				t.Errorf("PayloadLength = %v, want %v", frame.PayloadLength, tt.wantLength)
			}
			if offset != tt.wantOffset {
				t.Errorf("offset = %v, want %v", offset, tt.wantOffset)
			}
		})
	}
}

func TestFrameEncode(t *testing.T) {
	tests := []struct {
		name    string
		frame   frame
		wantLen int
	}{
		{
			name: "small payload",
			frame: frame{
				FIN:           true,
				OPCODE:        OpcodeText,
				PayloadLength: 5,
				IsMasked:      false,
			},
			wantLen: 2, // header only
		},
		{
			name: "small payload with mask",
			frame: frame{
				FIN:           true,
				OPCODE:        OpcodeText,
				PayloadLength: 5,
				IsMasked:      true,
			},
			wantLen: 6, // header + 4 byte mask
		},
		{
			name: "medium payload",
			frame: frame{
				FIN:           true,
				OPCODE:        OpcodeBinary,
				PayloadLength: 256,
				IsMasked:      false,
			},
			wantLen: 4, // header + 2 byte length
		},
		{
			name: "large payload",
			frame: frame{
				FIN:           true,
				OPCODE:        OpcodeBinary,
				PayloadLength: 65536,
				IsMasked:      false,
			},
			wantLen: 10, // header + 8 byte length
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.frame.encode()
			if err != nil {
				t.Errorf("encode() error = %v", err)
				return
			}

			if len(tt.frame.Encoded) != tt.wantLen {
				t.Errorf("encoded length = %v, want %v", len(tt.frame.Encoded), tt.wantLen)
			}

			// Check FIN bit
			if tt.frame.FIN && (tt.frame.Encoded[0]&0x80) == 0 {
				t.Error("FIN bit not set")
			}
			if !tt.frame.FIN && (tt.frame.Encoded[0]&0x80) != 0 {
				t.Error("FIN bit incorrectly set")
			}

			// Check opcode
			if tt.frame.Encoded[0]&0x0F != tt.frame.OPCODE {
				t.Errorf("opcode = %v, want %v", tt.frame.Encoded[0]&0x0F, tt.frame.OPCODE)
			}

			// Check mask bit
			if tt.frame.IsMasked && (tt.frame.Encoded[1]&0x80) == 0 {
				t.Error("mask bit not set")
			}
			if !tt.frame.IsMasked && (tt.frame.Encoded[1]&0x80) != 0 {
				t.Error("mask bit incorrectly set")
			}
		})
	}
}

func TestReadLength(t *testing.T) {
	tests := []struct {
		name       string
		raw        []byte
		wantLength int
		wantError  bool
	}{
		{
			name:       "small unmasked payload",
			raw:        []byte{0x81, 0x05, 'h', 'e', 'l', 'l', 'o'},
			wantLength: 7, // 2 byte header + 5 byte payload
		},
		{
			name:       "small masked payload",
			raw:        []byte{0x81, 0x85, 0x12, 0x34, 0x56, 0x78, 'h', 'e', 'l', 'l', 'o'},
			wantLength: 11, // 2 byte header + 4 byte mask + 5 byte payload
		},
		{
			name:       "medium payload",
			raw:        []byte{0x81, 0x7E, 0x01, 0x00}, // 256 bytes
			wantLength: 260,                            // 2 byte header + 2 byte length + 256 byte payload
		},
		{
			name:      "incomplete header",
			raw:       []byte{0x81},
			wantError: true,
		},
		{
			name:      "incomplete extended length",
			raw:       []byte{0x81, 0x7E, 0x01},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			length, err := readLength(tt.raw)

			if tt.wantError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if length != tt.wantLength {
				t.Errorf("length = %v, want %v", length, tt.wantLength)
			}
		})
	}
}

func TestFrameValidation(t *testing.T) {
	tests := []struct {
		name   string
		frame  frame
		method string
		want   bool
	}{
		{
			name:   "valid text opcode",
			frame:  frame{OPCODE: OpcodeText},
			method: "isValidOpcode",
			want:   true,
		},
		{
			name:   "valid binary opcode",
			frame:  frame{OPCODE: OpcodeBinary},
			method: "isValidOpcode",
			want:   true,
		},
		{
			name:   "valid ping opcode",
			frame:  frame{OPCODE: OpcodePing},
			method: "isValidOpcode",
			want:   true,
		},
		{
			name:   "invalid opcode",
			frame:  frame{OPCODE: 0x0F},
			method: "isValidOpcode",
			want:   false,
		},
		{
			name:   "is text frame",
			frame:  frame{OPCODE: OpcodeText},
			method: "isText",
			want:   true,
		},
		{
			name:   "is not text frame",
			frame:  frame{OPCODE: OpcodeBinary},
			method: "isText",
			want:   false,
		},
		{
			name:   "is control frame - ping",
			frame:  frame{OPCODE: OpcodePing},
			method: "isControl",
			want:   true,
		},
		{
			name:   "is control frame - close",
			frame:  frame{OPCODE: OpcodeClose},
			method: "isControl",
			want:   true,
		},
		{
			name:   "is not control frame",
			frame:  frame{OPCODE: OpcodeText},
			method: "isControl",
			want:   false,
		},
		{
			name:   "valid control frame",
			frame:  frame{FIN: true, OPCODE: OpcodePing, PayloadLength: 10},
			method: "isValidControl",
			want:   true,
		},
		{
			name:   "invalid control frame - not FIN",
			frame:  frame{FIN: false, OPCODE: OpcodePing, PayloadLength: 10},
			method: "isValidControl",
			want:   false,
		},
		{
			name:   "invalid control frame - too long payload",
			frame:  frame{FIN: true, OPCODE: OpcodePing, PayloadLength: 126},
			method: "isValidControl",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got bool
			switch tt.method {
			case "isValidOpcode":
				got = tt.frame.isValidOpcode()
			case "isText":
				got = tt.frame.isText()
			case "isControl":
				got = tt.frame.isControl()
			case "isValidControl":
				got = tt.frame.isValidControl()
			default:
				t.Fatalf("unknown method: %s", tt.method)
			}

			if got != tt.want {
				t.Errorf("%s() = %v, want %v", tt.method, got, tt.want)
			}
		})
	}
}

func TestMessageValidation(t *testing.T) {
	tests := []struct {
		name    string
		message message
		method  string
		want    bool
	}{
		{
			name: "valid UTF-8 text message",
			message: message{
				OPCODE:  OpcodeText,
				Payload: bytes.NewBufferString("Hello, 世界"),
			},
			method: "isValidUTF8",
			want:   true,
		},
		{
			name: "invalid UTF-8 text message",
			message: message{
				OPCODE:  OpcodeText,
				Payload: bytes.NewBuffer([]byte{0xFF, 0xFE}),
			},
			method: "isValidUTF8",
			want:   false,
		},
		{
			name: "is text message",
			message: message{
				OPCODE: OpcodeText,
			},
			method: "isText",
			want:   true,
		},
		{
			name: "is not text message",
			message: message{
				OPCODE: OpcodeBinary,
			},
			method: "isText",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got bool
			switch tt.method {
			case "isValidUTF8":
				got = tt.message.isValidUTF8()
			case "isText":
				got = tt.message.isText()
			default:
				t.Fatalf("unknown method: %s", tt.method)
			}

			if got != tt.want {
				t.Errorf("%s() = %v, want %v", tt.method, got, tt.want)
			}
		})
	}
}

func TestIsValidCloseCode(t *testing.T) {
	tests := []struct {
		name string
		code uint16
		want bool
	}{
		{"normal closure", CloseNormalClosure, true},
		{"going away", CloseGoingAway, true},
		{"protocol error", CloseProtocolError, true},
		{"unsupported data", CloseUnsupportedData, true},
		{"invalid frame payload", CloseInvalidFramePayloadData, true},
		{"policy violation", ClosePolicyViolation, true},
		{"message too big", CloseMessageTooBig, true},
		{"mandatory extension", CloseMandatoryExtension, true},
		{"internal server error", CloseInternalServerErr, true},
		{"invalid code", uint16(1234), false},
		{"reserved code", uint16(1004), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidCloseCode(tt.code)
			if got != tt.want {
				t.Errorf("isValidCloseCode(%v) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}

func TestFrameMask(t *testing.T) {
	// Create a frame with known masking key and payload
	frame := &frame{
		IsMasked:      true,
		PayloadLength: 3,
		PayloadOffset: 6,
		Encoded:       []byte{0x82, 0x83, 0x12, 0x34, 0x56, 0x78, 0x01, 0x02, 0x03},
	}

	// Apply masking
	frame.mask()

	// Check that payload is masked
	expected := []byte{0x01 ^ 0x12, 0x02 ^ 0x34, 0x03 ^ 0x56}
	actual := frame.payload()

	if !bytes.Equal(actual, expected) {
		t.Errorf("masked payload = %v, want %v", actual, expected)
	}
}

func TestFramePayload(t *testing.T) {
	frame := &frame{
		PayloadOffset: 2,
		PayloadLength: 5,
		Encoded:       []byte{0x81, 0x05, 'h', 'e', 'l', 'l', 'o'},
	}

	payload := frame.payload()
	expected := []byte{'h', 'e', 'l', 'l', 'o'}

	if !bytes.Equal(payload, expected) {
		t.Errorf("payload() = %v, want %v", payload, expected)
	}
}

func TestFrameCalcLength(t *testing.T) {
	tests := []struct {
		name     string
		frame    frame
		expected int
	}{
		{
			name: "small unmasked payload",
			frame: frame{
				PayloadLength: 5,
				IsMasked:      false,
			},
			expected: 7, // 2 + 5
		},
		{
			name: "small masked payload",
			frame: frame{
				PayloadLength: 5,
				IsMasked:      true,
			},
			expected: 11, // 2 + 4 + 5
		},
		{
			name: "medium payload",
			frame: frame{
				PayloadLength: 256,
				IsMasked:      false,
			},
			expected: 260, // 2 + 2 + 256
		},
		{
			name: "large payload",
			frame: frame{
				PayloadLength: 65536,
				IsMasked:      false,
			},
			expected: 65546, // 2 + 8 + 65536
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.frame.calcLength()
			if got != tt.expected {
				t.Errorf("calcLength() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// Benchmark tests
func BenchmarkNewFrame(b *testing.B) {
	payload := make([]byte, 1024)
	rand.Read(payload)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := newFrame(true, OpcodeText, false, payload)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadFrame(b *testing.B) {
	// Create a test frame
	frame, _ := newFrame(true, OpcodeText, true, []byte("hello world"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := readFrame(frame.Encoded)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFrameEncode(b *testing.B) {
	frame := frame{
		FIN:           true,
		OPCODE:        OpcodeText,
		PayloadLength: 1024,
		IsMasked:      false,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := frame.encode()
		if err != nil {
			b.Fatal(err)
		}
	}
}
