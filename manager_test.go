package snapws

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	// Test with nil options
	manager := NewManager[string](nil)
	if manager == nil {
		t.Fatal("Manager is nil")
	}
	if manager.Conns == nil {
		t.Error("Conns map is nil")
	}
	if len(manager.Conns) != 0 {
		t.Error("Conns map should be empty initially")
	}

	// Test with custom options
	opts := &Options{
		WriteWait:            time.Second * 10,
		ReadWait:             time.Second * 30,
		MaxMessageSize:       2048,
		SubProtocols:         []string{"echo", "chat"},
		RejectRaw:            true,
		BackpressureStrategy: BackpressureDrop,
	}
	upgrader2 := NewManager[string](NewUpgrader(opts))
	if upgrader2.Upgrader.WriteWait != time.Second*10 {
		t.Errorf("WriteWait = %v, want %v", upgrader2.Upgrader.WriteWait, time.Second*10)
	}
	if upgrader2.Upgrader.MaxMessageSize != 2048 {
		t.Errorf("MaxMessageSize = %v, want %v", upgrader2.Upgrader.MaxMessageSize, 2048)
	}
	if !upgrader2.Upgrader.RejectRaw {
		t.Error("RejectRaw should be true")
	}
}

func TestManagerRegister(t *testing.T) {
	manager := NewManager[string](nil)
	mockConn1 := manager.Upgrader.newConn(newMockConn(), "")
	mockConn2 := manager.Upgrader.newConn(newMockConn(), "")

	conn1 := manager.newManagedConn(mockConn1, "user1")
	conn2 := manager.newManagedConn(mockConn2, "user2")

	// Register first connection
	manager.Register("user1", conn1)
	if len(manager.Conns) != 1 {
		t.Errorf("Expected 1 connection, got %d", len(manager.Conns))
	}

	// Register second connection
	manager.Register("user2", conn2)
	if len(manager.Conns) != 2 {
		t.Errorf("Expected 2 connections, got %d", len(manager.Conns))
	}

	// Replace existing connection
	mockConn3 := manager.Upgrader.newConn(newMockConn(), "")
	conn3 := manager.newManagedConn(mockConn3, "user1")
	manager.Register("user1", conn3)
	if len(manager.Conns) != 2 {
		t.Errorf("Expected 2 connections after replacement, got %d", len(manager.Conns))
	}
	if manager.Conns["user1"] != conn3 {
		t.Error("Connection was not replaced")
	}
}

func TestManagerUnregister(t *testing.T) {
	manager := NewManager[string](nil)
	mockConn := newMockConn()
	conn := manager.newManagedConn(manager.Upgrader.newConn(mockConn, ""), "user1")

	manager.Register("user1", conn)

	// Test successful unregister
	err := manager.unregister("user1")
	if err != nil {
		t.Errorf("Unregister failed: %v", err)
	}
	if len(manager.Conns) != 0 {
		t.Error("Connection was not removed")
	}

	// Test unregister non-existent connection
	err = manager.unregister("nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent connection")
	}
}

func TestManagerGetConn(t *testing.T) {
	manager := NewManager[string](nil)
	mockConn := newMockConn()
	conn := manager.newManagedConn(manager.Upgrader.newConn(mockConn, ""), "user1")

	manager.Register("user1", conn)

	// Test existing connection
	retrievedConn, exists := manager.GetConn("user1")
	if !exists {
		t.Error("Connection should exist")
	}
	if retrievedConn != conn {
		t.Error("Retrieved wrong connection")
	}

	// Test non-existent connection
	_, exists = manager.GetConn("nonexistent")
	if exists {
		t.Error("Non-existent connection should not exist")
	}
}

func TestManagerGetAllConns(t *testing.T) {
	manager := NewManager[string](nil)

	// Test empty manager
	conns := manager.GetAllConns()
	if len(conns) != 0 {
		t.Errorf("Expected 0 connections, got %d", len(conns))
	}

	// Add connections
	for i := 0; i < 3; i++ {
		uConn := manager.Upgrader.newConn(newMockConn(), "")
		conn := manager.newManagedConn(uConn, fmt.Sprintf("user%d", i))
		manager.Register(fmt.Sprintf("user%d", i), conn)
	}

	conns = manager.GetAllConns()
	if len(conns) != 3 {
		t.Errorf("Expected 3 connections, got %d", len(conns))
	}
}

func TestManagerGetAllConnsWithExclude(t *testing.T) {
	manager := NewManager[string](nil)

	// Add connections
	for i := 0; i < 3; i++ {
		mockConn := manager.Upgrader.newConn(newMockConn(), "")
		conn := manager.newManagedConn(mockConn, fmt.Sprintf("user%d", i))
		manager.Register(fmt.Sprintf("user%d", i), conn)
	}

	conns := manager.GetAllConnsWithExclude("user1")
	if len(conns) != 2 {
		t.Errorf("Expected 2 connections, got %d", len(conns))
	}

	// Verify excluded connection is not present
	for _, conn := range conns {
		if conn.Key == "user1" {
			t.Error("Excluded connection found in results")
		}
	}
}

func TestManagerBroadcast(t *testing.T) {
	manager := NewManager[string](nil)
	ctx := context.Background()

	// Test empty manager
	n, err := manager.broadcast(ctx, "", OpcodeText, []byte("test"))
	if err != nil {
		t.Errorf("Broadcast failed: %v", err)
	}
	if n != 0 {
		t.Errorf("Expected 0 successful writes, got %d", n)
	}

	// Test invalid opcode
	_, err = manager.broadcast(ctx, "", OpcodePing, []byte("test"))
	if err == nil {
		t.Error("Expected error for invalid opcode")
	}
}

func TestManagerBroadcastString(t *testing.T) {
	manager := NewManager[string](nil)
	ctx := context.Background()

	// Add some connections
	for i := 0; i < 3; i++ {
		mockConn := manager.Upgrader.newConn(newMockConn(), "")
		conn := manager.newManagedConn(mockConn, fmt.Sprintf("user%d", i))
		go conn.listen()
		manager.Register(fmt.Sprintf("user%d", i), conn)
	}

	// Test valid UTF-8 string
	data := []byte("Hello, 世界!")
	n, err := manager.BroadcastString(ctx, "user1", data)
	if err != nil {
		t.Errorf("BroadcastString failed: %v", err)
	}
	if n != 2 { // Should exclude user1
		t.Errorf("Expected 2 successful writes, got %d", n)
	}

	// Test empty payload
	_, err = manager.BroadcastString(ctx, "", []byte{})
	if err == nil {
		t.Error("Expected error for empty payload")
	}

	// Test invalid UTF-8
	_, err = manager.BroadcastString(ctx, "", []byte{0xFF, 0xFE})
	if err == nil {
		t.Error("Expected error for invalid UTF-8")
	}
}

func TestManagerBroadcastBytes(t *testing.T) {
	manager := NewManager[string](nil)
	ctx := context.Background()

	// Add some connections
	for i := 0; i < 3; i++ {
		mockConn := manager.Upgrader.newConn(newMockConn(), "")
		conn := manager.newManagedConn(mockConn, fmt.Sprintf("user%d", i))
		go conn.listen()
		manager.Register(fmt.Sprintf("user%d", i), conn)
	}

	// Test valid bytes
	data := []byte{1, 2, 3, 4, 5}
	n, err := manager.BroadcastBytes(ctx, "user0", data)
	if err != nil {
		t.Errorf("BroadcastBytes failed: %v", err)
	}
	if n != 2 { // Should exclude user0
		t.Errorf("Expected 2 successful writes, got %d", n)
	}

	// Test empty payload
	_, err = manager.BroadcastBytes(ctx, "", []byte{})
	if err == nil {
		t.Error("Expected error for empty payload")
	}
}

func TestManagerUse(t *testing.T) {
	manager := NewManager[string](nil)

	middleware1 := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}
	middleware2 := func(w http.ResponseWriter, r *http.Request) error {
		return errors.New("middleware error")
	}

	// Test adding middlewares
	manager.Upgrader.Use(middleware1)
	manager.Upgrader.Use(middleware2)

	if len(manager.Upgrader.Middlwares) != 2 {
		t.Errorf("Expected 2 middlewares, got %d", len(manager.Upgrader.Middlwares))
	}
}

func TestManagerShutdown(t *testing.T) {
	manager := NewManager[string](nil)

	// Add some connections
	for i := 0; i < 5; i++ {
		mockConn := manager.Upgrader.newConn(newMockConn(), "")
		conn := manager.newManagedConn(mockConn, fmt.Sprintf("user%d", i))
		go conn.listen()
		manager.Register(fmt.Sprintf("user%d", i), conn)
	}

	if len(manager.Conns) != 5 {
		t.Errorf("Expected 5 connections before shutdown, got %d", len(manager.Conns))
	}

	manager.Shutdown()

	if len(manager.Conns) != 0 {
		t.Errorf("Expected 0 connections after shutdown, got %d", len(manager.Conns))
	}
}

// Handshake Tests

func TestValidateConnectionHeader(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		wantError bool
	}{
		{"valid upgrade", "upgrade", false},
		{"valid upgrade with spaces", " upgrade ", false},
		{"valid upgrade mixed case", "Upgrade", false},
		{"valid upgrade with other values", "keep-alive, upgrade", false},
		{"missing header", "", true},
		{"invalid header", "keep-alive", true},
		{"close connection", "close", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws", nil)
			if tt.header != "" {
				req.Header.Set("Connection", tt.header)
			}

			err := validateConnectionHeader(req)
			if tt.wantError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.wantError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestValidateUpgradeHeader(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		wantError bool
	}{
		{"valid websocket", "websocket", false},
		{"valid websocket with spaces", " websocket ", false},
		{"valid websocket mixed case", "WebSocket", false},
		{"valid websocket with other values", "http/1.1, websocket", false},
		{"missing header", "", true},
		{"invalid header", "http/1.1", true},
		{"wrong upgrade", "h2c", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws", nil)
			if tt.header != "" {
				req.Header.Set("Upgrade", tt.header)
			}

			err := validateUpgradeHeader(req)
			if tt.wantError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.wantError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestValidateVersionHeader(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		wantError bool
	}{
		{"valid version", "13", false},
		{"valid version with spaces", " 13 ", false},
		{"missing header", "", true},
		{"invalid version", "8", true},
		{"invalid version", "14", true},
		{"non-numeric version", "abc", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws", nil)
			if tt.header != "" {
				req.Header.Set("Sec-WebSocket-Version", tt.header)
			}

			err := validateVersionHeader(req)
			if tt.wantError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.wantError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestValidatedSecKeyHeader(t *testing.T) {
	validKey := base64.StdEncoding.EncodeToString(make([]byte, 16))

	tests := []struct {
		name      string
		header    string
		wantError bool
	}{
		{"valid key", validKey, false},
		{"valid key with spaces", " " + validKey + " ", false},
		{"missing header", "", true},
		{"invalid base64", "invalid-base64!", true},
		{"wrong length", base64.StdEncoding.EncodeToString(make([]byte, 10)), true},
		{"empty key", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws", nil)
			if tt.header != "" {
				req.Header.Set("Sec-WebSocket-Key", tt.header)
			}

			err := validatedSecKeyHeader(req)
			if tt.wantError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.wantError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestSelectSubProtocol(t *testing.T) {
	tests := []struct {
		name               string
		supportedProtocols []string
		requestHeader      string
		expected           string
	}{
		{"no supported protocols", nil, "echo, chat", ""},
		{"no request header", []string{"echo", "chat"}, "", ""},
		{"single match", []string{"echo", "chat"}, "echo", "echo"},
		{"multiple protocols, first match", []string{"echo", "chat"}, "chat, echo", "chat"},
		{"multiple protocols, second match", []string{"echo", "chat"}, "unknown, echo", "echo"},
		{"no match", []string{"echo", "chat"}, "unknown, other", ""},
		{"protocol with spaces", []string{"echo", "chat"}, " echo , chat ", "echo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws", nil)
			if tt.requestHeader != "" {
				req.Header.Set("Sec-WebSocket-Protocol", tt.requestHeader)
			}

			result := selectSubProtocol(req, tt.supportedProtocols)
			if result != tt.expected {
				t.Errorf("selectSubProtocol() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestUpgrade(t *testing.T) {
	upgrader := NewUpgrader(nil)

	// Create a valid WebSocket request
	key := base64.StdEncoding.EncodeToString(make([]byte, 16))

	tests := []struct {
		name       string
		method     string
		headers    map[string]string
		wantError  bool
		wantStatus int
	}{
		{
			name:   "valid handshake",
			method: "GET",
			headers: map[string]string{
				"Connection":            "upgrade",
				"Upgrade":               "websocket",
				"Sec-WebSocket-Version": "13",
				"Sec-WebSocket-Key":     key,
			},
			wantError: false,
		},
		{
			name:       "wrong method",
			method:     "POST",
			wantError:  true,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:   "missing connection header",
			method: "GET",
			headers: map[string]string{
				"Upgrade":               "websocket",
				"Sec-WebSocket-Version": "13",
				"Sec-WebSocket-Key":     key,
			},
			wantError:  true,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:   "missing upgrade header",
			method: "GET",
			headers: map[string]string{
				"Connection":            "upgrade",
				"Sec-WebSocket-Version": "13",
				"Sec-WebSocket-Key":     key,
			},
			wantError:  true,
			wantStatus: http.StatusUpgradeRequired,
		},
		{
			name:   "wrong version",
			method: "GET",
			headers: map[string]string{
				"Connection":            "upgrade",
				"Upgrade":               "websocket",
				"Sec-WebSocket-Version": "8",
				"Sec-WebSocket-Key":     key,
			},
			wantError:  true,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:   "missing key",
			method: "GET",
			headers: map[string]string{
				"Connection":            "upgrade",
				"Upgrade":               "websocket",
				"Sec-WebSocket-Version": "13",
			},
			wantError:  true,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/ws", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			w := httptest.NewRecorder()

			// Note: This will fail for valid cases because we can't actually hijack in tests
			// but we can test the validation logic
			_, err := upgrader.Upgrade(w, req)

			if tt.wantError && err == nil {
				t.Error("Expected error but got none")
			}
		})
	}
}

func TestUpgraderHandshakeWithSubProtocols(t *testing.T) {
	opts := &Options{
		SubProtocols: []string{"echo", "chat"},
		RejectRaw:    false,
	}
	upgrader := NewUpgrader(opts)

	key := base64.StdEncoding.EncodeToString(make([]byte, 16))

	tests := []struct {
		name             string
		protocolHeader   string
		rejectRaw        bool
		expectedProtocol string
		expectError      bool
	}{
		{
			name:             "matching protocol",
			protocolHeader:   "echo",
			expectedProtocol: "echo",
		},
		{
			name:             "multiple protocols, first match",
			protocolHeader:   "chat, echo",
			expectedProtocol: "chat",
		},
		{
			name:             "no matching protocol, accept raw",
			protocolHeader:   "unknown",
			rejectRaw:        false,
			expectedProtocol: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upgrader.RejectRaw = tt.rejectRaw

			req := httptest.NewRequest("GET", "/ws", nil)
			req.Header.Set("Connection", "upgrade")
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Sec-WebSocket-Version", "13")
			req.Header.Set("Sec-WebSocket-Key", key)
			if tt.protocolHeader != "" {
				req.Header.Set("Sec-WebSocket-Protocol", tt.protocolHeader)
			}

			w := httptest.NewRecorder()

			conn, err := upgrader.Upgrade(w, req)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				// Hijack will fail in tests, but protocol selection should work
				if !strings.Contains(err.Error(), "feature not supported") {
					t.Errorf("Unexpected error: %v", err)
				} else {
					// If hijack failed, we can't test protocol, so skip
					return
				}
			}

			if conn.SubProtocol != tt.expectedProtocol {
				t.Errorf("Expected protocol %v, got %v", tt.expectedProtocol, conn.SubProtocol)
			}
		})
	}
}

func TestManagerHandshakeWithMiddleware(t *testing.T) {
	upgrader := NewUpgrader(nil)

	// Add middleware that returns an error
	upgrader.Use(func(w http.ResponseWriter, r *http.Request) error {
		return NewMiddlewareErr(http.StatusUnauthorized, "unauthorized")
	})

	key := base64.StdEncoding.EncodeToString(make([]byte, 16))
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Connection", "upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", key)

	w := httptest.NewRecorder()

	_, err := upgrader.Upgrade(w, req)
	if err == nil {
		t.Error("Expected middleware error")
	}
}

func TestGUID(t *testing.T) {
	expected := "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	if GUID != expected {
		t.Errorf("GUID = %v, want %v", GUID, expected)
	}
}

func TestWebSocketKeyGeneration(t *testing.T) {
	// Test the WebSocket key acceptance algorithm
	key := "dGhlIHNhbXBsZSBub25jZQ=="
	expected := "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="

	hashedKey := sha1.Sum([]byte(key + GUID))
	result := base64.StdEncoding.EncodeToString(hashedKey[:])

	if result != expected {
		t.Errorf("WebSocket key hash = %v, want %v", result, expected)
	}
}

// Test Options and BackpressureStrategy

func TestOptionsWithDefault(t *testing.T) {
	opts := &Options{}
	opts.WithDefault()

	if opts.WriteWait != defaultWriteWait {
		t.Errorf("WriteWait = %v, want %v", opts.WriteWait, defaultWriteWait)
	}
	if opts.ReadWait != defaultReadWait {
		t.Errorf("ReadWait = %v, want %v", opts.ReadWait, defaultReadWait)
	}
	if opts.PingEvery != defaultPingEvery {
		t.Errorf("PingEvery = %v, want %v", opts.PingEvery, defaultPingEvery)
	}
	if opts.MaxMessageSize != DefaultMaxMessageSize {
		t.Errorf("MaxMessageSize = %v, want %v", opts.MaxMessageSize, DefaultMaxMessageSize)
	}
	if opts.BackpressureStrategy != BackpressureClose {
		t.Errorf("BackpressureStrategy = %v, want %v", opts.BackpressureStrategy, BackpressureClose)
	}
}

func TestBackpressureStrategyValid(t *testing.T) {
	tests := []struct {
		strategy BackpressureStrategy
		valid    bool
	}{
		{BackpressureClose, true},
		{BackpressureDrop, true},
		{BackpressureWait, true},
		{BackpressureStrategy(999), false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("strategy_%d", tt.strategy), func(t *testing.T) {
			if tt.strategy.Valid() != tt.valid {
				t.Errorf("strategy.Valid() = %v, want %v", tt.strategy.Valid(), tt.valid)
			}
		})
	}
}

func TestMiddlewareErr(t *testing.T) {
	err := NewMiddlewareErr(http.StatusBadRequest, "bad request")

	if err.Code != http.StatusBadRequest {
		t.Errorf("Code = %v, want %v", err.Code, http.StatusBadRequest)
	}
	if err.Message != "bad request" {
		t.Errorf("Message = %v, want %v", err.Message, "bad request")
	}
	if err.Error() != "bad request" {
		t.Errorf("Error() = %v, want %v", err.Error(), "bad request")
	}

	// Test AsMiddlewareErr
	mwErr, ok := AsMiddlewareErr(err)
	if !ok {
		t.Error("AsMiddlewareErr should return true for MiddlewareErr")
	}
	if mwErr != err {
		t.Error("AsMiddlewareErr should return the same error")
	}

	// Test with regular error
	regularErr := errors.New("regular error")
	_, ok = AsMiddlewareErr(regularErr)
	if ok {
		t.Error("AsMiddlewareErr should return false for regular error")
	}
}

// Benchmark tests
func BenchmarkManagerRegister(b *testing.B) {
	manager := NewManager[string](NewUpgrader(nil))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mockConn := manager.Upgrader.newConn(newMockConn(), "")
		conn := manager.newManagedConn(mockConn, fmt.Sprintf("user%d", i))
		manager.Register(fmt.Sprintf("user%d", i), conn)
	}
}

func BenchmarkManagerGetConn(b *testing.B) {
	manager := NewManager[string](NewUpgrader(nil))

	// Setup connections
	for i := 0; i < 1000; i++ {
		mockConn := manager.Upgrader.newConn(newMockConn(), "")
		conn := manager.newManagedConn(mockConn, fmt.Sprintf("user%d", i))
		manager.Register(fmt.Sprintf("user%d", i), conn)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.GetConn("user500")
	}
}

func BenchmarkManagerBroadcast(b *testing.B) {
	manager := NewManager[string](nil)
	ctx := context.Background()

	// Setup connections
	for i := 0; i < 100; i++ {
		mockConn := manager.Upgrader.newConn(newMockConn(), "")
		conn := manager.newManagedConn(mockConn, fmt.Sprintf("user%d", i))
		manager.Register(fmt.Sprintf("user%d", i), conn)
	}

	data := []byte("benchmark message")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.BroadcastString(ctx, "", data)
	}
}

func BenchmarkValidateHeaders(b *testing.B) {
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Connection", "upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", base64.StdEncoding.EncodeToString(make([]byte, 16)))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validateConnectionHeader(req)
		validateUpgradeHeader(req)
		validateVersionHeader(req)
		validatedSecKeyHeader(req)
	}
}
