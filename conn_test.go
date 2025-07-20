package snapws

import (
	"context"
	"net"
	"testing"
	"time"
)

// --- 1. Dummy net.Conn implementation ---
type nopConn struct{}

func (nopConn) Read(b []byte) (int, error)         { return 0, nil }
func (nopConn) Write(b []byte) (int, error)        { return len(b), nil }
func (nopConn) Close() error                       { return nil }
func (nopConn) LocalAddr() net.Addr                { return nil }
func (nopConn) RemoteAddr() net.Addr               { return nil }
func (nopConn) SetDeadline(t time.Time) error      { return nil }
func (nopConn) SetReadDeadline(t time.Time) error  { return nil }
func (nopConn) SetWriteDeadline(t time.Time) error { return nil }

// --- 2. Fake Manager ---
func newTestManager() *Manager[string] {
	return &Manager[string]{
		Args: Args[string]{
			WriteBufferSize:     4096,
			WriterTimeout:       time.Second,
			PingEvery:           time.Minute,
			InboundFramesSize:   16,
			InboundMessagesSize: 16,
			OutboundFramesSize:  10000,
			OutboundControlSize: 8,
		},
	}
}

// --- 3. Dummy Frame and NewFrame() ---
type Frame1 struct {
	Payload []byte
}

func NewFrame1(fin bool, opcode uint8, rsv1 bool, payload []byte) (Frame1, error) {
	return Frame1{Payload: payload}, nil
}

// --- 4. Benchmark ---
var benchSink interface{}

func BenchmarkWriter_Reuse(b *testing.B) {
	manager := newTestManager()
	conn := manager.newConn(nopConn{}, "bench", "")

	go func() {
		for req := range conn.outboundFrames {
			req.errCh <- nil
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*100)
	defer cancel()
	payload := make([]byte, 3000)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w, err := conn.NextWriter(ctx, OpcodeText)
		if err != nil {
			b.Fatal(err)
		}

		n, err := w.Write(payload)
		if err != nil {
			b.Fatal(err)
		}

		err = w.Close()
		if err != nil {
			b.Fatal(err)
		}

		// Use the result so compiler can't optimize away
		benchSink = n
	}
}

// --- 5. Benchmark: small messages (256 bytes) ---
func BenchmarkWriter_SmallMessages(b *testing.B) {
	manager := newTestManager()
	conn := manager.newConn(nopConn{}, "bench", "")

	go func() {
		for req := range conn.outboundFrames {
			req.errCh <- nil
		}
	}()

	ctx := context.Background()
	payload := make([]byte, 256)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w, err := conn.NextWriter(ctx, OpcodeText)
		if err != nil {
			b.Fatal(err)
		}

		_, err = w.Write(payload)
		if err != nil {
			b.Fatal(err)
		}

		if err := w.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

// --- 6. Benchmark: very large messages (16 KB) ---
func BenchmarkWriter_LargeMessages(b *testing.B) {
	manager := newTestManager()
	conn := manager.newConn(nopConn{}, "bench", "")

	go func() {
		for req := range conn.outboundFrames {
			req.errCh <- nil
		}
	}()

	ctx := context.Background()
	payload := make([]byte, 16*1024) // 16KB

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w, err := conn.NextWriter(ctx, OpcodeBinary)
		if err != nil {
			b.Fatal(err)
		}

		_, err = w.Write(payload)
		if err != nil {
			b.Fatal(err)
		}

		if err := w.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

// --- 7. Benchmark: multiple small writes before flush ---
func BenchmarkWriter_MultiWriteBeforeFlush(b *testing.B) {
	manager := newTestManager()
	conn := manager.newConn(nopConn{}, "bench", "")

	go func() {
		for req := range conn.outboundFrames {
			req.errCh <- nil
		}
	}()

	ctx := context.Background()
	chunk := make([]byte, 512)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w, err := conn.NextWriter(ctx, OpcodeText)
		if err != nil {
			b.Fatal(err)
		}

		for j := 0; j < 8; j++ { // multiple writes adding up to ~4KB
			_, err = w.Write(chunk)
			if err != nil {
				b.Fatal(err)
			}
		}

		if err := w.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

// --- 8. Benchmark: flush without writes (empty frame flush) ---
func BenchmarkWriter_EmptyFlush(b *testing.B) {
	manager := newTestManager()
	conn := manager.newConn(nopConn{}, "bench", "")

	go func() {
		for req := range conn.outboundFrames {
			req.errCh <- nil
		}
	}()

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w, err := conn.NextWriter(ctx, OpcodeText)
		if err != nil {
			b.Fatal(err)
		}

		// No Write call here

		if err := w.Close(); err != nil {
			b.Fatal(err)
		}
	}
}
