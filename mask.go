package snapws

import (
	"encoding/binary"
	"unsafe"
)

func (r *connReader) unMask(b []byte) {
	r.unMaskStd(b)
}

func (cw *controlWriter) unMask(p []byte) {
	for i := range p {
		p[i] = p[i] ^ cw.maskKey[i%4]
	}
}

func (r *connReader) unMaskStd(p []byte) {
	for i := range p {
		p[i] = p[i] ^ r.maskKey[r.maskPos%4]
		r.maskPos++
	}
}

// copied from Coder/websocket, added it to test different masking methods
func (r *connReader) unMaskCoder(b []byte) {
	key := binary.BigEndian.Uint32(r.maskKey[:])
	if len(b) >= 8 {
		key64 := uint64(key)<<32 | uint64(key)

		// At some point in the future we can clean these unrolled loops up.
		// See https://github.com/golang/go/issues/31586#issuecomment-487436401

		// Then we xor until b is less than 128 bytes.
		for len(b) >= 128 {
			v := binary.BigEndian.Uint64(b)
			binary.BigEndian.PutUint64(b, v^key64)
			v = binary.BigEndian.Uint64(b[8:16])
			binary.BigEndian.PutUint64(b[8:16], v^key64)
			v = binary.BigEndian.Uint64(b[16:24])
			binary.BigEndian.PutUint64(b[16:24], v^key64)
			v = binary.BigEndian.Uint64(b[24:32])
			binary.BigEndian.PutUint64(b[24:32], v^key64)
			v = binary.BigEndian.Uint64(b[32:40])
			binary.BigEndian.PutUint64(b[32:40], v^key64)
			v = binary.BigEndian.Uint64(b[40:48])
			binary.BigEndian.PutUint64(b[40:48], v^key64)
			v = binary.BigEndian.Uint64(b[48:56])
			binary.BigEndian.PutUint64(b[48:56], v^key64)
			v = binary.BigEndian.Uint64(b[56:64])
			binary.BigEndian.PutUint64(b[56:64], v^key64)
			v = binary.BigEndian.Uint64(b[64:72])
			binary.BigEndian.PutUint64(b[64:72], v^key64)
			v = binary.BigEndian.Uint64(b[72:80])
			binary.BigEndian.PutUint64(b[72:80], v^key64)
			v = binary.BigEndian.Uint64(b[80:88])
			binary.BigEndian.PutUint64(b[80:88], v^key64)
			v = binary.BigEndian.Uint64(b[88:96])
			binary.BigEndian.PutUint64(b[88:96], v^key64)
			v = binary.BigEndian.Uint64(b[96:104])
			binary.BigEndian.PutUint64(b[96:104], v^key64)
			v = binary.BigEndian.Uint64(b[104:112])
			binary.BigEndian.PutUint64(b[104:112], v^key64)
			v = binary.BigEndian.Uint64(b[112:120])
			binary.BigEndian.PutUint64(b[112:120], v^key64)
			v = binary.BigEndian.Uint64(b[120:128])
			binary.BigEndian.PutUint64(b[120:128], v^key64)
			b = b[128:]
		}

		// Then we xor until b is less than 64 bytes.
		for len(b) >= 64 {
			v := binary.BigEndian.Uint64(b)
			binary.BigEndian.PutUint64(b, v^key64)
			v = binary.BigEndian.Uint64(b[8:16])
			binary.BigEndian.PutUint64(b[8:16], v^key64)
			v = binary.BigEndian.Uint64(b[16:24])
			binary.BigEndian.PutUint64(b[16:24], v^key64)
			v = binary.BigEndian.Uint64(b[24:32])
			binary.BigEndian.PutUint64(b[24:32], v^key64)
			v = binary.BigEndian.Uint64(b[32:40])
			binary.BigEndian.PutUint64(b[32:40], v^key64)
			v = binary.BigEndian.Uint64(b[40:48])
			binary.BigEndian.PutUint64(b[40:48], v^key64)
			v = binary.BigEndian.Uint64(b[48:56])
			binary.BigEndian.PutUint64(b[48:56], v^key64)
			v = binary.BigEndian.Uint64(b[56:64])
			binary.BigEndian.PutUint64(b[56:64], v^key64)
			b = b[64:]
		}

		// Then we xor until b is less than 32 bytes.
		for len(b) >= 32 {
			v := binary.BigEndian.Uint64(b)
			binary.BigEndian.PutUint64(b, v^key64)
			v = binary.BigEndian.Uint64(b[8:16])
			binary.BigEndian.PutUint64(b[8:16], v^key64)
			v = binary.BigEndian.Uint64(b[16:24])
			binary.BigEndian.PutUint64(b[16:24], v^key64)
			v = binary.BigEndian.Uint64(b[24:32])
			binary.BigEndian.PutUint64(b[24:32], v^key64)
			b = b[32:]
		}

		// Then we xor until b is less than 16 bytes.
		for len(b) >= 16 {
			v := binary.BigEndian.Uint64(b)
			binary.BigEndian.PutUint64(b, v^key64)
			v = binary.BigEndian.Uint64(b[8:16])
			binary.BigEndian.PutUint64(b[8:16], v^key64)
			b = b[16:]
		}

		// Then we xor until b is less than 8 bytes.
		for len(b) >= 8 {
			v := binary.BigEndian.Uint64(b)
			binary.BigEndian.PutUint64(b, v^key64)
			b = b[8:]
		}
	}

	// Then we xor until b is less than 4 bytes.
	for len(b) >= 4 {
		v := binary.BigEndian.Uint32(b)
		binary.BigEndian.PutUint32(b, v^key)
		b = b[4:]
	}

	// xor remaining bytes.
	for i := range b {
		b[i] ^= byte(key)
	}
}

// copied from gorilla/websocket, added it to test different masking methods
func (r *connReader) unMaskGorilla(p []byte) {
	mask32 := uint32(r.maskKey[0]) |
		uint32(r.maskKey[1])<<8 |
		uint32(r.maskKey[2])<<16 |
		uint32(r.maskKey[3])<<24

	n := len(p)
	i := 0
	for ; i+4 <= n; i += 4 {
		v := *(*uint32)(unsafe.Pointer(&p[i]))
		v ^= mask32
		*(*uint32)(unsafe.Pointer(&p[i])) = v
	}
	for ; i < n; i++ {
		p[i] ^= r.maskKey[i%4]
	}
}
