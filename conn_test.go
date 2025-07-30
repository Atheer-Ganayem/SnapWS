package snapws

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// Mock net.Conn for testing
type mockConn struct {
	readBuf    *bytes.Buffer
	writeBuf   *bytes.Buffer
	closed     bool
	mu         sync.Mutex
	allowReads bool
}

func newMockConn() *mockConn {
	return &mockConn{
		readBuf:  new(bytes.Buffer),
		writeBuf: new(bytes.Buffer),
	}
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	if !m.allowReads {
		time.Sleep(time.Second / 2)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, io.EOF
	}
	return m.readBuf.Read(b)
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, errors.New("connection closed")
	}
	return m.writeBuf.Write(b)
}

func (m *mockConn) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockConn) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (m *mockConn) RemoteAddr() net.Addr               { return &net.TCPAddr{} }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

func (m *mockConn) writeFrame(frame *frame) {
	if !m.allowReads {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readBuf.Write(frame.Encoded)
}

func createTestManager() *Manager[string] {
	opts := &Options{
		WriteWait:           time.Second,
		ReadWait:            time.Second * 5,
		PingEvery:           time.Second * 30,
		MaxMessageSize:      1024,
		ReadBufferSize:      256,
		WriteBufferSize:     256,
		InboundFramesSize:   10,
		InboundMessagesSize: 5,
		OutboundControlSize: 2,
	}
	return NewManager[string](NewUpgrader(opts))
}

func TestConnCreation(t *testing.T) {
	manager := createTestManager()
	mockConn := newMockConn()
	c := manager.Upgrader.newConn(mockConn, "")
	conn := manager.newManagedConn(c, "test-key")

	if conn.Key != "test-key" {
		t.Errorf("Key = %v, want %v", conn.Key, "test-key")
	}
	if conn.SubProtocol != "" {
		t.Errorf("SubProtocol = %v, want empty", conn.SubProtocol)
	}
	if conn.Manager != manager {
		t.Error("Manager not set correctly")
	}
	if conn.raw != mockConn {
		t.Error("Raw connection not set correctly")
	}
	if conn.isClosed.Load() {
		t.Error("Connection should not be closed initially")
	}
}

func TestConnClose(t *testing.T) {
	upgrader := NewUpgrader(nil)
	mockConn := newMockConn()
	conn := upgrader.newConn(mockConn, "")

	// Test normal close
	conn.Close()

	if !conn.isClosed.Load() {
		t.Error("Connection should be closed")
	}

	// Test multiple closes (should not panic)
	conn.Close()
	conn.Close()
}

func TestConnCloseWithCode(t *testing.T) {
	upgrader := NewUpgrader(nil)
	mockConn := newMockConn()
	conn := upgrader.newConn(mockConn, "")

	conn.CloseWithCode(CloseProtocolError, "test reason")

	if !conn.isClosed.Load() {
		t.Error("Connection should be closed")
	}

	// Verify close frame was written
	if mockConn.writeBuf.Len() == 0 {
		t.Error("Expected close frame to be written")
	}
}

func TestConnCloseWithPayload(t *testing.T) {
	tests := []struct {
		name           string
		payload        []byte
		expectClose    bool
		expectedCode   uint16
		expectedReason string
	}{
		{
			name:         "valid payload with reason",
			payload:      append([]byte{0x03, 0xE8}, []byte("test reason")...), // 1000 + reason
			expectClose:  true,
			expectedCode: CloseNormalClosure,
		},
		{
			name:        "valid payload without reason",
			payload:     []byte{0x03, 0xE8}, // 1000
			expectClose: true,
		},
		{
			name:        "invalid payload too short",
			payload:     []byte{0x03},
			expectClose: true,
		},
		{
			name:        "invalid close code",
			payload:     []byte{0x04, 0xD2}, // 1234 (invalid)
			expectClose: true,
		},
		{
			name:        "invalid UTF-8 reason",
			payload:     append([]byte{0x03, 0xE8}, []byte{0xFF, 0xFE}...),
			expectClose: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upgrader := NewUpgrader(nil)
			mockConn := newMockConn()
			conn := upgrader.newConn(mockConn, "")

			conn.CloseWithPayload(tt.payload)

			if tt.expectClose && !conn.isClosed.Load() {
				t.Error("Connection should be closed")
			}
		})
	}
}

func TestConnWriter(t *testing.T) {
	upgrader := NewUpgrader(nil)
	mockConn := newMockConn()
	conn := upgrader.newConn(mockConn, "")

	ctx := context.Background()

	// Test NextWriter
	writer, err := conn.NextWriter(ctx, OpcodeText)
	if err != nil {
		t.Fatalf("NextWriter failed: %v", err)
	}
	if writer == nil {
		t.Fatal("Writer is nil")
	}
	if writer.opcode != OpcodeText {
		t.Errorf("Writer opcode = %v, want %v", writer.opcode, OpcodeText)
	}

	// Test Write
	data := []byte("hello world")
	n, err := writer.Write(data)
	if err != nil {
		t.Errorf("Write failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("Write returned %v bytes, want %v", n, len(data))
	}

	// Test Close
	err = writer.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}
	if !writer.closed {
		t.Error("Writer should be closed")
	}
}

func TestConnWriterInvalidOpcode(t *testing.T) {
	upgrader := NewUpgrader(nil)
	mockConn := newMockConn()
	conn := upgrader.newConn(mockConn, "")

	ctx := context.Background()

	_, err := conn.NextWriter(ctx, OpcodePing) // Invalid for NextWriter
	if err == nil {
		t.Error("Expected error for invalid opcode")
	}
}

func TestConnWriterFlush(t *testing.T) {
	upgrader := NewUpgrader(nil)
	mockConn := newMockConn()
	conn := upgrader.newConn(mockConn, "")

	ctx := context.Background()
	writer, err := conn.NextWriter(ctx, OpcodeText)
	if err != nil {
		t.Errorf("Writer failed: %v", err)
	}

	// Write some data
	_, err = writer.Write([]byte("test"))
	if err != nil {
		t.Errorf("Write failed: %v", err)
	}

	// Test flush without FIN
	err = writer.Flush(false)
	if err != nil {
		t.Errorf("Flush failed: %v", err)
	}

	// Write more data
	_, err = writer.Write([]byte("data"))
	if err != nil {
		t.Errorf("Write failed: %v", err)
	}

	// Test flush with FIN
	err = writer.Flush(true)
	if err != nil {
		t.Errorf("Final flush failed: %v", err)
	}

	writer.Close()
}

func TestSendBytes(t *testing.T) {
	upgrader := NewUpgrader(nil)
	mockConn := newMockConn()
	conn := upgrader.newConn(mockConn, "")

	ctx := context.Background()
	data := []byte{1, 2, 3, 4, 5}

	// Test successful send
	err := conn.SendBytes(ctx, data)
	if err != nil {
		t.Errorf("SendBytes failed: %v", err)
	}

	// Test empty payload
	err = conn.SendBytes(ctx, []byte{})
	if err == nil {
		t.Error("Expected error for empty payload")
	}
}

func TestSendString(t *testing.T) {
	upgrader := NewUpgrader(nil)
	mockConn := newMockConn()
	conn := upgrader.newConn(mockConn, "")

	ctx := context.Background()
	data := []byte("hello world")

	// Test successful send
	err := conn.SendString(ctx, data)
	if err != nil {
		t.Errorf("SendString failed: %v", err)
	}

	// Test empty payload
	err = conn.SendString(ctx, []byte{})
	if err == nil {
		t.Error("Expected error for empty payload")
	}

	// Test invalid UTF-8
	err = conn.SendString(ctx, []byte{0xFF, 0xFE})
	if err == nil {
		t.Error("Expected error for invalid UTF-8")
	}
}

func TestSendJSON(t *testing.T) {
	upgrader := NewUpgrader(nil)
	mockConn := newMockConn()
	conn := upgrader.newConn(mockConn, "")

	ctx := context.Background()

	// Test successful send
	data := map[string]interface{}{
		"message": "hello",
		"count":   42,
	}
	err := conn.SendJSON(ctx, data)
	if err != nil {
		t.Errorf("SendJSON failed: %v", err)
	}

	// Test nil payload
	err = conn.SendJSON(ctx, nil)
	if err == nil {
		t.Error("Expected error for nil payload")
	}
}

func TestPingPong(t *testing.T) {
	upgrader := NewUpgrader(nil)
	mockConn := newMockConn()
	conn := upgrader.newConn(mockConn, "")

	// Test Ping
	err := conn.Ping()
	if err != nil {
		t.Errorf("Ping failed: %v", err)
	}

	// Test Pong
	payload := []byte("pong data")
	conn.Pong(payload) // Should not panic or error
}

func TestConnReader(t *testing.T) {
	upgrader := NewUpgrader(nil)
	mockConn := newMockConn()
	conn := upgrader.newConn(mockConn, "")

	// Create a test message
	testData := []byte("hello world")
	message := &message{
		OPCODE:  OpcodeText,
		Payload: bytes.NewBuffer(testData),
	}

	// Set up reader
	reader := &ConnReader{
		conn:    conn,
		message: message,
		eof:     false,
	}

	// Test Read
	buf := make([]byte, 5)
	n, err := reader.Read(buf)
	if err != nil {
		t.Errorf("Read failed: %v", err)
	}
	if n != 5 {
		t.Errorf("Read returned %v bytes, want 5", n)
	}
	if string(buf) != "hello" {
		t.Errorf("Read data = %s, want hello", string(buf))
	}

	// Test reading rest
	buf = make([]byte, 10)
	n, err = reader.Read(buf)
	if err != nil {
		t.Errorf("Read failed: %v", err)
	}
	if n != 6 {
		t.Errorf("Read returned %v bytes, want 6", n)
	}
	if string(buf[:n]) != " world" {
		t.Errorf("Read data = %s, want ' world'", string(buf[:n]))
	}

	// Test EOF
	n, err = reader.Read(buf)
	if err != io.EOF {
		t.Errorf("Expected EOF, got %v", err)
	}
	if n != 0 {
		t.Errorf("Read returned %v bytes, want 0", n)
	}
}

func TestConnReaderPayload(t *testing.T) {
	testData := []byte("test payload")
	message := &message{
		OPCODE:  OpcodeText,
		Payload: bytes.NewBuffer(testData),
	}

	reader := &ConnReader{
		message: message,
	}

	payload := reader.Payload()
	if !bytes.Equal(payload, testData) {
		t.Errorf("Payload = %v, want %v", payload, testData)
	}
}

func TestReadMessage(t *testing.T) {
	upgrader := NewUpgrader(nil)
	mockConn := newMockConn()
	conn := upgrader.newConn(mockConn, "")

	// Test with closed connection
	conn.isClosed.Store(true)
	ctx := context.Background()

	_, _, err := conn.ReadMessage(ctx)
	if err == nil {
		t.Error("Expected error for closed connection")
	}
}

func TestReadBinary(t *testing.T) {
	upgrader := NewUpgrader(nil)
	mockConn := newMockConn()
	conn := upgrader.newConn(mockConn, "")

	ctx := context.Background()

	// Test with closed connection
	conn.isClosed.Store(true)
	_, err := conn.ReadBinary(ctx)
	if err == nil {
		t.Error("Expected error for closed connection")
	}
}

func TestReadString(t *testing.T) {
	upgrader := NewUpgrader(nil)
	mockConn := newMockConn()
	conn := upgrader.newConn(mockConn, "")

	ctx := context.Background()

	// Test with closed connection
	conn.isClosed.Store(true)
	_, err := conn.ReadString(ctx)
	if err == nil {
		t.Error("Expected error for closed connection")
	}
}

func TestReadJSON(t *testing.T) {
	upgrader := NewUpgrader(nil)
	mockConn := newMockConn()
	conn := upgrader.newConn(mockConn, "")

	ctx := context.Background()

	// Test with closed connection
	conn.isClosed.Store(true)
	var result map[string]interface{}
	err := conn.ReadJSON(ctx, &result)
	if err == nil {
		t.Error("Expected error for closed connection")
	}
}

func TestWriteHeaders(t *testing.T) {
	upgrader := NewUpgrader(nil)
	mockConn := newMockConn()
	conn := upgrader.newConn(mockConn, "")

	ctx := context.Background()
	writer, _ := conn.NextWriter(ctx, OpcodeText)

	// Write some data first
	writer.Write([]byte("test"))

	// Test writeHeaders
	err := writer.writeHeaders(true, OpcodeText)
	if err != nil {
		t.Errorf("writeHeaders failed: %v", err)
	}

	writer.Close()
}

func TestLockUnlockW(t *testing.T) {
	upgrader := NewUpgrader(nil)
	mockConn := newMockConn()
	conn := upgrader.newConn(mockConn, "")

	ctx := context.Background()

	// Test successful lock
	err := conn.lockW(ctx)
	if err != nil {
		t.Errorf("lockW failed: %v", err)
	}

	// Test unlock
	err = conn.unlockW()
	if err != nil {
		t.Errorf("unlockW failed: %v", err)
	}

	// Test double unlock (should not error)
	err = conn.unlockW()
	if err != nil {
		t.Errorf("Double unlockW failed: %v", err)
	}
}

func TestSendMessageToChan(t *testing.T) {
	tests := []struct {
		name     string
		strategy BackpressureStrategy
	}{
		{"BackpressureClose", BackpressureClose},
		{"BackpressureDrop", BackpressureDrop},
		// {"BackpressureWait", BackpressureWait},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &Options{
				BackpressureStrategy: tt.strategy,
				InboundMessagesSize:  1,
			}
			upgrader := NewUpgrader(opts)
			mockConn := newMockConn()
			conn := upgrader.newConn(mockConn, "")

			message := &message{
				OPCODE:  OpcodeText,
				Payload: bytes.NewBufferString("test"),
			}

			// Fill the channel first
			conn.inboundMessages <- message

			// This should trigger backpressure handling
			conn.sendMessageToChan(message)

			// Clean up
			<-conn.inboundMessages
		})
	}
}

// Benchmark tests
func BenchmarkConnWrite(b *testing.B) {
	upgrader := NewUpgrader(nil)
	mockConn := newMockConn()
	conn := upgrader.newConn(mockConn, "")

	ctx := context.Background()
	data := make([]byte, 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		writer, err := conn.NextWriter(ctx, OpcodeText)
		if err != nil {
			b.Fatalf("next writer err: %s", err)
		}
		writer.Write(data)
		writer.Close()
	}
}

func BenchmarkSendBytes(b *testing.B) {
	upgrader := NewUpgrader(nil)
	mockConn := newMockConn()
	conn := upgrader.newConn(mockConn, "")

	ctx := context.Background()
	data := make([]byte, 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn.SendBytes(ctx, data)
	}
}

func BenchmarkSendString(b *testing.B) {
	upgrader := NewUpgrader(nil)
	mockConn := newMockConn()
	conn := upgrader.newConn(mockConn, "")

	ctx := context.Background()
	data := bytes.Repeat([]byte("a"), 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn.SendString(ctx, data)
	}
}

// package snapws

// import (
// 	"bytes"
// 	"context"
// 	"io"
// 	"net"
// 	"testing"
// 	"time"
// )

// // --- 1. Dummy net.Conn implementation ---
// type nopConn struct {
// 	data []byte
// 	pos  int
// }

// func (m nopConn) Read(b []byte) (int, error) {
// 	if m.pos >= len(m.data) {
// 		return 0, io.EOF
// 	}
// 	n := copy(b, m.data[m.pos:])
// 	m.pos += n
// 	return n, nil
// }
// func (nopConn) Write(b []byte) (int, error)        { return len(b), nil }
// func (nopConn) Close() error                       { return nil }
// func (nopConn) LocalAddr() net.Addr                { return nil }
// func (nopConn) RemoteAddr() net.Addr               { return nil }
// func (nopConn) SetDeadline(t time.Time) error      { return nil }
// func (nopConn) SetReadDeadline(t time.Time) error  { return nil }
// func (nopConn) SetWriteDeadline(t time.Time) error { return nil }

// // --- 2. Fake Manager ---
// func newTestManager() *Manager[string] {
// 	return &Manager[string]{
// 		Options: Options[string]{
// 			WriteBufferSize:     4096,
// 			PingEvery:           time.Minute,
// 			InboundFramesSize:   16,
// 			InboundMessagesSize: 16,
// 			OutboundControlSize: 8,
// 		},
// 	}
// }

// // --- 4. Benchmark ---
// var benchSink interface{}

// func BenchmarkWriter_Reuse(b *testing.B) {
// 	manager := newTestManager()
// 	conn := manager.newConn(nopConn{}, "bench", "")

// 	go func() {
// 		for range conn.writer.sig {
// 			conn.writer.errCh <- nil
// 		}
// 	}()

// 	ctx, cancel := context.WithTimeout(context.Background(), time.Second*100)
// 	defer cancel()
// 	payload := make([]byte, 3000)

// 	b.ResetTimer()
// 	b.ReportAllocs()

// 	for i := 0; i < b.N; i++ {
// 		w, err := conn.NextWriter(ctx, OpcodeText)
// 		if err != nil {
// 			b.Fatal(err)
// 		}

// 		n, err := w.Write(payload)
// 		if err != nil {
// 			b.Fatal(err)
// 		}

// 		err = w.Close()
// 		if err != nil {
// 			b.Fatal(err)
// 		}

// 		// Use the result so compiler can't optimize away
// 		benchSink = n
// 	}
// }

// // --- 5. Benchmark: small messages (256 bytes) ---
// func BenchmarkWriter_SmallMessages(b *testing.B) {
// 	manager := newTestManager()
// 	conn := manager.newConn(nopConn{}, "bench", "")

// 	go func() {
// 		for range conn.writer.sig {
// 			conn.writer.errCh <- nil
// 		}
// 	}()

// 	ctx := context.Background()
// 	payload := make([]byte, 256)

// 	b.ResetTimer()
// 	b.ReportAllocs()

// 	for i := 0; i < b.N; i++ {
// 		w, err := conn.NextWriter(ctx, OpcodeText)
// 		if err != nil {
// 			b.Fatal(err)
// 		}

// 		_, err = w.Write(payload)
// 		if err != nil {
// 			b.Fatal(err)
// 		}

// 		if err := w.Close(); err != nil {
// 			b.Fatal(err)
// 		}
// 	}
// }

// // --- 6. Benchmark: very large messages (16 KB) ---
// func BenchmarkWriter_LargeMessages(b *testing.B) {
// 	manager := newTestManager()
// 	conn := manager.newConn(nopConn{}, "bench", "")

// 	go func() {
// 		for range conn.writer.sig {
// 			conn.writer.errCh <- nil
// 		}
// 	}()

// 	ctx := context.Background()
// 	payload := make([]byte, 16*1024) // 16KB

// 	b.ResetTimer()
// 	b.ReportAllocs()

// 	for i := 0; i < b.N; i++ {
// 		w, err := conn.NextWriter(ctx, OpcodeBinary)
// 		if err != nil {
// 			b.Fatal(err)
// 		}

// 		_, err = w.Write(payload)
// 		if err != nil {
// 			b.Fatal(err)
// 		}

// 		if err := w.Close(); err != nil {
// 			b.Fatal(err)
// 		}
// 	}
// }

// // --- 7. Benchmark: multiple small writes before flush ---
// func BenchmarkWriter_MultiWriteBeforeFlush(b *testing.B) {
// 	manager := newTestManager()
// 	conn := manager.newConn(nopConn{}, "bench", "")

// 	go func() {
// 		for range conn.writer.sig {
// 			conn.writer.errCh <- nil
// 		}
// 	}()

// 	ctx := context.Background()
// 	chunk := make([]byte, 512)

// 	b.ResetTimer()
// 	b.ReportAllocs()

// 	for i := 0; i < b.N; i++ {
// 		w, err := conn.NextWriter(ctx, OpcodeText)
// 		if err != nil {
// 			b.Fatal(err)
// 		}

// 		for j := 0; j < 8; j++ { // multiple writes adding up to ~4KB
// 			_, err = w.Write(chunk)
// 			if err != nil {
// 				b.Fatal(err)
// 			}
// 		}

// 		if err := w.Close(); err != nil {
// 			b.Fatal(err)
// 		}
// 	}
// }

// // --- 8. Benchmark: flush without writes (empty frame flush) ---
// func BenchmarkWriter_EmptyFlush(b *testing.B) {
// 	manager := newTestManager()
// 	conn := manager.newConn(nopConn{}, "bench", "")

// 	go func() {
// 		for range conn.writer.sig {
// 			conn.writer.errCh <- nil
// 		}
// 	}()

// 	ctx := context.Background()

// 	b.ResetTimer()
// 	b.ReportAllocs()

// 	for i := 0; i < b.N; i++ {
// 		w, err := conn.NextWriter(ctx, OpcodeText)
// 		if err != nil {
// 			b.Fatal(err)
// 		}

// 		// No Write call here

// 		if err := w.Close(); err != nil {
// 			b.Fatal(err)
// 		}
// 	}
// }

// // --- Dummy Reader Setup ---

// // simulate a fully read message with a buffer of certain size
// func newTestMessage(size int) *message {
// 	payload := bytes.NewBuffer(make([]byte, size))
// 	for i := 0; i < size; i++ {
// 		payload.Bytes()[i] = byte(i % 256)
// 	}
// 	return &message{
// 		OPCODE:  OpcodeBinary,
// 		Payload: payload,
// 	}
// }

// // simulate inboundMessages channel with pre-filled messages
// func setupInboundMessages(conn *Conn[string], size int, count int) {
// 	go func() {
// 		for i := 0; i < count; i++ {
// 			conn.inboundMessages <- newTestMessage(size)
// 		}
// 	}()
// }

// // --- Benchmarks ---

// func BenchmarkReader_SmallMessage_ReadAll(b *testing.B) {
// 	manager := newTestManager()
// 	conn := manager.newConn(nopConn{}, "bench", "")
// 	msgSize := 512

// 	setupInboundMessages(conn, msgSize, b.N)

// 	ctx, cancel := context.WithTimeout(context.Background(), time.Second*100)
// 	defer cancel()

// 	b.ResetTimer()
// 	b.ReportAllocs()

// 	for i := 0; i < b.N; i++ {
// 		msgType, data, err := conn.ReadMessage(ctx)
// 		if err != nil {
// 			b.Fatal(err)
// 		}
// 		if msgType != OpcodeBinary || len(data) != msgSize {
// 			b.Fatalf("unexpected read message size/type")
// 		}
// 	}
// }

// func BenchmarkReader_LargeMessage_ReadAll(b *testing.B) {
// 	manager := newTestManager()
// 	conn := manager.newConn(nopConn{}, "bench", "")
// 	msgSize := 10000

// 	setupInboundMessages(conn, msgSize, b.N)

// 	ctx, cancel := context.WithTimeout(context.Background(), time.Second*100)
// 	defer cancel()

// 	b.ResetTimer()
// 	b.ReportAllocs()

// 	for i := 0; i < b.N; i++ {
// 		msgType, data, err := conn.ReadMessage(ctx)
// 		if err != nil {
// 			b.Fatal(err)
// 		}
// 		if msgType != OpcodeBinary || len(data) != msgSize {
// 			b.Fatalf("unexpected read message size/type")
// 		}
// 	}
// }

// func BenchmarkReader_ConnReader_Read(b *testing.B) {
// 	manager := newTestManager()
// 	conn := manager.newConn(nopConn{}, "bench", "")
// 	msgSize := 2048

// 	setupInboundMessages(conn, msgSize, b.N)

// 	ctx, cancel := context.WithTimeout(context.Background(), time.Second*100)
// 	defer cancel()

// 	b.ResetTimer()
// 	b.ReportAllocs()

// 	for i := 0; i < b.N; i++ {
// 		reader, msgType, err := conn.NextReader(ctx)
// 		if err != nil {
// 			b.Fatal(err)
// 		}
// 		if msgType != OpcodeBinary {
// 			b.Fatalf("unexpected message type")
// 		}

// 		buf := make([]byte, 512)
// 		totalRead := 0
// 		for {
// 			n, err := reader.Read(buf)
// 			totalRead += n
// 			if err == io.EOF {
// 				break
// 			} else if err != nil {
// 				b.Fatal(err)
// 			}
// 		}
// 		if totalRead != msgSize {
// 			b.Fatalf("read size mismatch: got %d want %d", totalRead, msgSize)
// 		}
// 	}
// }

// // /////////////
// // ///////////
// // //////
// // --- Benchmark for acceptFrame ---

// func BenchmarkAcceptFrame(b *testing.B) {
// 	manager := NewManager[string](nil)

// 	// Create an in-memory full-duplex connection
// 	s1, s2 := net.Pipe()

// 	// Simulate the remote sending a valid frame
// 	go func() {
// 		defer s2.Close()

// 		payload := []byte("hello")
// 		// payload := make([]byte, 8192)
// 		frame, _ := newFrame(true, OpcodeText, true, payload)
// 		frame.mask()
// 		c := make([]byte, len(frame.Encoded))

// 		for i := 0; i < b.N; i++ {
// 			copy(c, frame.Encoded)
// 			_, err := s2.Write(c)
// 			if err != nil {
// 				panic(err)
// 			}
// 		}
// 	}()

// 	// Use the manager to wrap the raw conn
// 	conn := manager.newConn(s1, "s1", "")

// 	// Wait for data to arrive
// 	time.Sleep(time.Second)

// 	b.ResetTimer()
// 	for i := 0; i < b.N; i++ {
// 		frame, code, err := conn.acceptFrame()
// 		if err != nil {
// 			b.Fatalf("acceptFrame failed: %v", err)
// 		}
// 		if code != 0 || !bytes.Equal(frame.payload(), []byte("hello")) {
// 			b.Fatalf("unexpected frame data")
// 		}
// 	}
// }

// func BenchmarkAcceptMessage(b *testing.B) {
// 	manager := NewManager[string](nil)

// 	s1, s2 := net.Pipe()

// 	conn := manager.newConn(s1, "s1", "")
// 	go conn.readLoop()
// 	go func() {
// 		defer s2.Close()
// 		payload := []byte("hello")
// 		frame, _ := newFrame(true, OpcodeText, true, payload)
// 		frame.mask()
// 		c := make([]byte, len(frame.Encoded))

// 		for i := 0; i < b.N; i++ {
// 			copy(c, frame.Encoded)
// 			_, err := s2.Write(c)
// 			if err != nil {
// 				panic(err)
// 			}
// 		}
// 		conn.Close()
// 	}()

// 	b.ResetTimer()
// 	conn.acceptMessage()
// }
