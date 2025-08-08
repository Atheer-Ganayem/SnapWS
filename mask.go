package snapws

import (
	"encoding/binary"
	"unsafe"
)

func mask(b []byte, key []byte, pos int) int {
	return maskGorilla(b, key, pos)
}

func (r *ConnReader) unMask(b []byte) {
	newpos := mask(b, r.maskKey[:], r.maskPos)
	r.maskPos = newpos % 4
}

func (cw *ControlWriter) unMask(b []byte) {
	_ = mask(b, cw.maskKey[:], 0)
}

func maskGorilla(b []byte, key []byte, pos int) int {
	if len(b) == 0 {
		return 0
	}

	i := 0
	n := len(b)

	// Handle unaligned start bytes
	for (pos+i)%4 != 0 && i < n {
		b[i] ^= key[(pos+i)%4]
		i++
	}

	// Create 32-bit mask to mask in chuncks
	mask32 := uint32(key[0]) |
		uint32(key[1])<<8 |
		uint32(key[2])<<16 |
		uint32(key[3])<<24

	// Process 4 bytes at a time
	for i+4 <= n {
		v := *(*uint32)(unsafe.Pointer(&b[i]))
		v ^= mask32
		*(*uint32)(unsafe.Pointer(&b[i])) = v
		i += 4
	}

	// Handle remaining bytes
	for i < n {
		b[i] ^= key[(pos+i)%4]
		i++
	}

	return pos + len(b) // Return new position
}

func maskSafe(b []byte, key [4]byte, pos int) int {
	for i := 0; i < len(b); i++ {
		b[i] ^= key[(pos+i)%4]
	}
	return pos + len(b)
}

// copied from Coder/websocket, added it to test different masking methods
func unMaskCoder(b []byte, maskKey [4]byte) {
	key := binary.BigEndian.Uint32(maskKey[:])
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
