package snapws

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Mock connection for testing
type mockConn struct {
	readBuf  *bytes.Buffer
	writeBuf *bytes.Buffer
	closed   atomic.Bool
	mu       sync.Mutex
}

func newMockConn() *mockConn {
	return &mockConn{
		readBuf:  bytes.NewBuffer(nil),
		writeBuf: bytes.NewBuffer(nil),
	}
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	if m.closed.Load() {
		return 0, io.EOF
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.readBuf.Read(b)
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	if m.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeBuf.Write(b)
}

func (m *mockConn) Close() error {
	m.closed.Store(true)
	return nil
}

func (m *mockConn) LocalAddr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080} }
func (m *mockConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}
}
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

// Helper to create a test connection
func createTestConn() *Conn {
	upgrader := NewUpgrader(&Options{
		WriteWait:       time.Second,
		ReadWait:        time.Second,
		PingEvery:       time.Second * 30,
		MaxMessageSize:  1024 * 1024,
		WriteBufferSize: 4096,
	})

	mockConn := newMockConn()
	br := bufio.NewReader(mockConn)

	return upgrader.newConn(mockConn, "", br, make([]byte, 0, 4096))
}

// Test basic connection creation and properties
func TestConn_Creation(t *testing.T) {
	conn := createTestConn()
	defer conn.Close()

	if conn == nil {
		t.Fatal("Expected non-nil connection")
	}

	if !conn.isServer {
		t.Error("Expected isServer to be true")
	}

	if conn.SubProtocol != "" {
		t.Errorf("Expected empty subprotocol, got %s", conn.SubProtocol)
	}

	select {
	case <-conn.done:
		t.Error("Expected connection to not be closed initially")
	default:
	}
}

// Test connection close
func TestConn_Close(t *testing.T) {
	conn1 := createTestConn()
	conn2 := createTestConn()

	conn1.Close()
	conn2.CloseWithCode(CloseNormalClosure, "test")

	select {
	case <-conn1.done:
	case <-conn2.done:
	default:
		t.Error("Expected connection to be closed")
	}

	raw, ok := conn2.raw.(*mockConn)
	if !ok {
		t.Error("Raw conn conversion failed")
	}

	b := make([]byte, 256)
	n, err := raw.writeBuf.Read(b)
	if err != io.EOF && err != nil {
		t.Fatal(err)
	}

	// headers + close code + message
	if n != 2+2+4 {
		t.Errorf("Unexpected payload length: %d", n)
	}

	fin := b[0]&0b10000000 == 128
	rsv1 := b[0] & 0b01000000
	rsv2 := b[0] & 0b00100000
	rsv3 := b[0] & 0b00010000
	opcode := b[0] & 0b00001111
	isMasked := b[1]&0b10000000 == 128
	length := b[1] & 0b01111111

	if !fin || (rsv1|rsv2|rsv3) != 0 || opcode != OpcodeClose || isMasked || length != 6 {
		t.Error("Unexpected headers")
	}

	code := binary.BigEndian.Uint16(b[2:4])
	if code != CloseNormalClosure {
		t.Errorf("Unexpected close code: %d", code)
	}

	if string(b[4:n]) != "test" {
		t.Errorf("Unexpected close mesage: %s", b[4:n])
	}
}

// Test NextWriter basic functionality
func TestConn_NextWriter(t *testing.T) {
	conn := createTestConn()
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Test getting a text writer
	writer, err := conn.NextWriter(ctx, OpcodeText)
	if err != nil {
		t.Fatalf("Expected no error getting writer, got %v", err)
	}

	if writer == nil {
		t.Fatal("Expected non-nil writer")
	}

	if writer.opcode != OpcodeText {
		t.Errorf("Expected opcode %d, got %d", OpcodeText, writer.opcode)
	}

	if writer.closed {
		t.Error("Expected writer to not be closed")
	}

	// Close the writer
	err = writer.Close()
	if err != nil {
		t.Errorf("Expected no error closing writer, got %v", err)
	}

	if !writer.closed {
		t.Error("Expected writer to be closed after Close()")
	}
}

// Test NextWriter with invalid opcode
func TestConn_NextWriter_InvalidOpcode(t *testing.T) {
	conn := createTestConn()
	defer conn.Close()

	ctx := context.Background()

	// Test with control frame opcode (should fail)
	_, err := conn.NextWriter(ctx, OpcodeClose)
	if err == nil {
		t.Error("Expected error with control frame opcode")
	}

	// Test with invalid opcode
	_, err = conn.NextWriter(ctx, 255)
	if err == nil {
		t.Error("Expected error with invalid opcode")
	}
}

// Test Writer Write functionality
func TestConnWriter_Write(t *testing.T) {
	conn := createTestConn()
	defer conn.Close()

	ctx := context.Background()
	writer, err := conn.NextWriter(ctx, OpcodeText)
	if err != nil {
		t.Fatalf("Failed to get writer: %v", err)
	}
	defer writer.Close()

	data := []byte("Hello, WebSocket!")
	n, err := writer.Write(data)
	if err != nil {
		t.Fatalf("Failed to write data: %v", err)
	}

	if n != len(data) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(data), n)
	}

	// Test writing to closed writer
	writer.Close()
	_, err = writer.Write([]byte("should fail"))
	if err == nil {
		t.Error("Expected error writing to closed writer")
	}
}

// Test send methods
func TestConn_SendMessage(t *testing.T) {
	conn := createTestConn()
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Test text message
	err := conn.SendMessage(ctx, OpcodeText, []byte("Hello"))
	if err != nil {
		t.Errorf("Failed to send text message: %v", err)
	}

	// Test binary message
	err = conn.SendMessage(ctx, OpcodeBinary, []byte{0x01, 0x02, 0x03})
	if err != nil {
		t.Errorf("Failed to send binary message: %v", err)
	}

	// Test with invalid opcode
	err = conn.SendMessage(ctx, OpcodeClose, []byte("invalid"))
	if err == nil {
		t.Error("Expected error with control frame opcode")
	}

	err = conn.SendString(ctx, []byte("send string"))
	if err != nil {
		t.Errorf("Failed to send string: %v", err)
	}
	err = conn.SendBytes(ctx, []byte("send bytes"))
	if err != nil {
		t.Errorf("Failed to send bytes: %v", err)
	}
	err = conn.SendJSON(ctx, []byte("send json"))
	if err != nil {
		t.Errorf("Failed to send json: %v", err)
	}
}

// test SkipUTF8Validation=false
func TestConn_UTF8Validation(t *testing.T) {
	// Test with validation enabled
	upgrader := NewUpgrader(&Options{
		SkipUTF8Validation: false,
		WriteWait:          time.Second,
		ReadWait:           time.Second,
	})

	mockConn := newMockConn()
	br := bufio.NewReader(mockConn)

	invalidUTF8Frame := []byte{0x81, 0x81, 0x01, 0x02, 0x03, 0x04, 0xFE}
	mockConn.readBuf.Write(invalidUTF8Frame)

	conn := upgrader.newConn(mockConn, "", br, make([]byte, 0, 4096))
	defer conn.Close()

	_, err := conn.ReadString()
	if !(err != nil && IsFatalErr(err) && err.Error() == ErrInvalidUTF8.Error()) {
		t.Error("should faild uft8")
	}
}

// test SkipUTF8Validation=true
func TestConn_UTF8Validation_skip(t *testing.T) {
	// Test with validation enabled
	upgrader := NewUpgrader(&Options{
		SkipUTF8Validation: true,
		WriteWait:          time.Second,
		ReadWait:           time.Second,
	})

	mockConn := newMockConn()
	br := bufio.NewReader(mockConn)

	invalidUTF8Frame := []byte{0x81, 0x81, 0x01, 0x02, 0x03, 0x04, 0xFE}
	mockConn.readBuf.Write(invalidUTF8Frame)

	conn := upgrader.newConn(mockConn, "", br, make([]byte, 0, 4096))
	defer conn.Close()

	_, err := conn.ReadString()
	if err != nil {
		t.Error("should not fail")
	}
}

// Test connection metadata
func TestConn_MetaData(t *testing.T) {
	conn := createTestConn()
	defer conn.Close()

	// Test setting and getting metadata
	conn.MetaData.Store("user_id", "12345")

	userID, ok := conn.MetaData.Load("user_id")
	if !ok {
		t.Error("Expected to find user_id in metadata")
	}
	if userID != "12345" {
		t.Errorf("Expected user_id to be '12345', got %v", userID)
	}
}

// Test NetConn method
func TestConn_NetConn(t *testing.T) {
	conn := createTestConn()
	defer conn.Close()

	netConn := conn.NetConn()
	if netConn == nil {
		t.Error("Expected non-nil net.Conn")
	}

	// Test that it's the same underlying connection
	if netConn != conn.raw {
		t.Error("NetConn() should return the same connection as conn.raw")
	}
}

// Test concurrent writer access (should fail)
func TestConn_ConcurrentWriterAccess(t *testing.T) {
	conn := createTestConn()
	defer conn.Close()

	ctx := context.Background()

	writer1, err := conn.NextWriter(ctx, OpcodeText)
	if err != nil {
		t.Fatalf("Failed to get first writer: %v", err)
	}

	// Try to get another writer while first is still active
	ctx2, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = conn.NextWriter(ctx2, OpcodeText)
	if err == nil {
		t.Error("Expected error when trying to get concurrent writers")
	}

	// Close first writer
	writer1.Close()

	// Now getting a new writer should work
	writer2, err := conn.NextWriter(ctx, OpcodeText)
	if err != nil {
		t.Errorf("Should be able to get writer after closing previous one: %v", err)
	}
	writer2.Close()
}

// Test writing laege data
func TestConnWriter_BufferManagement(t *testing.T) {
	conn := createTestConn()
	defer conn.Close()

	ctx := context.Background()
	writer, err := conn.NextWriter(ctx, OpcodeText)
	if err != nil {
		t.Fatalf("Failed to get writer: %v", err)
	}
	defer writer.Close()

	// Write data that should fill buffer and trigger flush
	largeData := make([]byte, conn.upgrader.WriteBufferSize*2)

	n, err := writer.Write(largeData)
	if err != nil {
		t.Errorf("Failed to write large data: %v", err)
	}

	if n != len(largeData) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(largeData), n)
	}
}

// Test ManagedConn
func TestManagedConn(t *testing.T) {
	manager := NewManager[string](nil)
	conn := createTestConn()
	key := "test_key"

	managedConn := manager.newManagedConn(conn, key)

	if managedConn.Key != key {
		t.Errorf("Expected key %s, got %s", key, managedConn.Key)
	}

	if managedConn.Manager != manager {
		t.Error("Expected manager to be set correctly")
	}

	if managedConn.Conn != conn {
		t.Error("Expected conn to be set correctly")
	}
}

// Test helpers
func TestHelpers(t *testing.T) {
	// Test comparePayload
	p1 := []byte("hello")
	p2 := []byte("hello")
	p3 := []byte("world")

	if !comparePayload(p1, p2) {
		t.Error("Expected identical payloads to be equal")
	}

	if comparePayload(p1, p3) {
		t.Error("Expected different payloads to not be equal")
	}
}
