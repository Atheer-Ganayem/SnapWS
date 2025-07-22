package snapws

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// --- 1. Dummy net.Conn implementation ---
type nopConn struct {
	data []byte
	pos  int
}

func (m nopConn) Read(b []byte) (int, error) {
	if m.pos >= len(m.data) {
		return 0, io.EOF
	}
	n := copy(b, m.data[m.pos:])
	m.pos += n
	return n, nil
}
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

// --- 4. Benchmark ---
var benchSink interface{}

func BenchmarkWriter_Reuse(b *testing.B) {
	manager := newTestManager()
	conn := manager.newConn(nopConn{}, "bench", "")

	go func() {
		for range conn.writer.sig {
			conn.writer.errCh <- nil
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
		for range conn.writer.sig {
			conn.writer.errCh <- nil
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
		for range conn.writer.sig {
			conn.writer.errCh <- nil
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
		for range conn.writer.sig {
			conn.writer.errCh <- nil
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
		for range conn.writer.sig {
			conn.writer.errCh <- nil
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

// --- Dummy Reader Setup ---

// simulate a fully read message with a buffer of certain size
func newTestMessage(size int) *Message {
	payload := bytes.NewBuffer(make([]byte, size))
	for i := 0; i < size; i++ {
		payload.Bytes()[i] = byte(i % 256)
	}
	return &Message{
		OPCODE:  OpcodeBinary,
		Payload: payload,
	}
}

// simulate inboundMessages channel with pre-filled messages
func setupInboundMessages(conn *Conn[string], size int, count int) {
	go func() {
		for i := 0; i < count; i++ {
			conn.inboundMessages <- newTestMessage(size)
		}
	}()
}

// --- Benchmarks ---

func BenchmarkReader_SmallMessage_ReadAll(b *testing.B) {
	manager := newTestManager()
	conn := manager.newConn(nopConn{}, "bench", "")
	msgSize := 512

	setupInboundMessages(conn, msgSize, b.N)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*100)
	defer cancel()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		msgType, data, err := conn.ReadMessage(ctx)
		if err != nil {
			b.Fatal(err)
		}
		if msgType != OpcodeBinary || len(data) != msgSize {
			b.Fatalf("unexpected read message size/type")
		}
	}
}

func BenchmarkReader_LargeMessage_ReadAll(b *testing.B) {
	manager := newTestManager()
	conn := manager.newConn(nopConn{}, "bench", "")
	msgSize := 10000

	setupInboundMessages(conn, msgSize, b.N)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*100)
	defer cancel()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		msgType, data, err := conn.ReadMessage(ctx)
		if err != nil {
			b.Fatal(err)
		}
		if msgType != OpcodeBinary || len(data) != msgSize {
			b.Fatalf("unexpected read message size/type")
		}
	}
}

func BenchmarkReader_ConnReader_Read(b *testing.B) {
	manager := newTestManager()
	conn := manager.newConn(nopConn{}, "bench", "")
	msgSize := 2048

	setupInboundMessages(conn, msgSize, b.N)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*100)
	defer cancel()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		reader, msgType, err := conn.NextReader(ctx)
		if err != nil {
			b.Fatal(err)
		}
		if msgType != OpcodeBinary {
			b.Fatalf("unexpected message type")
		}

		buf := make([]byte, 512)
		totalRead := 0
		for {
			n, err := reader.Read(buf)
			totalRead += n
			if err == io.EOF {
				break
			} else if err != nil {
				b.Fatal(err)
			}
		}
		if totalRead != msgSize {
			b.Fatalf("read size mismatch: got %d want %d", totalRead, msgSize)
		}
	}
}

// /////////////
// ///////////
// //////
// --- Benchmark for acceptFrame ---

func BenchmarkAcceptFrame(b *testing.B) {
	manager := NewManager[string](nil)

	// Create an in-memory full-duplex connection
	s1, s2 := net.Pipe()

	// Simulate the remote sending a valid frame
	go func() {
		defer s2.Close()

		payload := []byte("hello")
		// payload := make([]byte, 8192)
		frame, _ := NewFrame(true, OpcodeText, true, payload)
		frame.Mask()
		c := make([]byte, len(frame.Encoded))

		for i := 0; i < b.N; i++ {
			copy(c, frame.Encoded)
			_, err := s2.Write(c)
			if err != nil {
				panic(err)
			}
		}
	}()

	// Use the manager to wrap the raw conn
	conn := manager.newConn(s1, "s1", "")

	// Wait for data to arrive
	time.Sleep(time.Second)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		frame, code, err := conn.acceptFrame()
		if err != nil {
			b.Fatalf("acceptFrame failed: %v", err)
		}
		if code != 0 || !bytes.Equal(frame.Payload(), []byte("hello")) {
			b.Fatalf("unexpected frame data")
		}
	}
}

func BenchmarkAcceptMessage(b *testing.B) {
	manager := NewManager[string](nil)

	s1, s2 := net.Pipe()

	conn := manager.newConn(s1, "s1", "")
	go conn.readLoop()
	go func() {
		defer s2.Close()
		payload := []byte("hello")
		frame, _ := NewFrame(true, OpcodeText, true, payload)
		frame.Mask()
		c := make([]byte, len(frame.Encoded))

		for i := 0; i < b.N; i++ {
			copy(c, frame.Encoded)
			_, err := s2.Write(c)
			if err != nil {
				panic(err)
			}
		}
		conn.Close()
	}()

	b.ResetTimer()
	conn.acceptMessage()
}
