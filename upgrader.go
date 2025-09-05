package snapws

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
)

const GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// Used to upgrader HTTP connections to Websocket connections.
// Hold snap.Options. If you wanna learn more about the options go see their docs.
type Upgrader struct {
	*Options
	writePool sync.Pool
	Limiter   *RateLimiter
	Flusher   *batchFlusher
}

// Created a new upgrader with the given options.
// If options is nil, then it will assign a new options with default values.
func NewUpgrader(opts *Options) *Upgrader {
	if opts == nil {
		opts = &Options{}
	}
	opts.WithDefault()

	u := &Upgrader{
		Options: opts,
	}
	if !opts.DisableWriteBuffersPooling {
		u.writePool = sync.Pool{
			New: func() any {
				return &pooledBuf{make([]byte, u.WriteBufferSize)}
			},
		}
	}

	return u
}

// Upgrades an HTTP connection to a Websocket connection.
// Receives (w http.ResponseWriter, r *http.Request) and returns a pointer to a snapws.Conn and an err.
// It checks method, headers, and selects an appopiate sub-protocol and runs the middlewares and the
// onConnect hook if they exist, and finnaly it responds to the client (both if the upgrade succeeds or fails).
func (u *Upgrader) Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	// validate method and headers
	if r.Method != http.MethodGet {
		http.Error(w, "request method should be get", http.StatusMethodNotAllowed)
		return nil, ErrWrongMethod
	}

	if err := validateUpgradeHeader(r); err != nil {
		http.Error(w, "invalid or missing upgrade header", http.StatusUpgradeRequired)
		return nil, err
	}
	if err := validateConnectionHeader(r); err != nil {
		http.Error(w, "invalid or missing connection header", http.StatusBadRequest)
		return nil, err
	}
	if err := validateVersionHeader(r); err != nil {
		http.Error(w, "invalid or missing Sec-WebSocket-Version header, must be 13", http.StatusBadRequest)
		return nil, err
	}
	if err := validatedSecKeyHeader(r); err != nil {
		http.Error(w, "invalid or missing Sec-WebSocket-Key header", http.StatusBadRequest)
		return nil, err
	}

	// selecting a subprotocol
	subProtocol := selectSubProtocol(r, u.SubProtocols)
	if subProtocol == "" && u.RejectRaw {
		http.Error(w, "unsupported or missing subprotocol", http.StatusBadRequest)
		return nil, ErrUnsupportedSubProtocols
	}

	// running middlewares
	if u.Middlwares != nil {
		for _, middleware := range u.Middlwares {
			if err := middleware(w, r); err != nil {
				if mwErr, ok := AsMiddlewareErr(err); ok {
					http.Error(w, mwErr.Message, mwErr.Code)
				}
				http.Error(w, "middleware error", http.StatusBadRequest)
				return nil, err
			}
		}
	}

	// computing key
	hashedKey := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + GUID))
	secAcceptKey := base64.StdEncoding.EncodeToString(hashedKey[:])

	// hijacking connection
	c, brw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		http.Error(w, "failed to hijack connection", http.StatusInternalServerError)
		return nil, err
	}

	p := brw.Writer.AvailableBuffer()
	conn := u.newConn(c, subProtocol, brw.Reader, p)

	// writing response
	p = append(p, "HTTP/1.1 101 Switching Protocols\r\n"...)
	p = append(p, "Upgrade: websocket\r\n"...)
	p = append(p, "Connection: Upgrade\r\n"...)
	p = append(p, "Sec-WebSocket-Accept: "...)
	p = append(p, secAcceptKey...)
	p = append(p, "\r\n"...)
	if subProtocol != "" {
		p = append(p, "Sec-WebSocket-Protocol: "...)
		p = append(p, subProtocol...)
		p = append(p, "\r\n"...)
	}
	p = append(p, "\r\n"...)

	_, err = c.Write(p)
	if err != nil {
		return nil, err
	}

	if u.OnConnect != nil {
		u.OnConnect(conn)
	}

	if u.Limiter != nil {
		u.Limiter.addClient(conn)
	}

	return conn, err
}

func validateConnectionHeader(r *http.Request) error {
	rawHeader := r.Header.Get("Connection")
	if rawHeader == "" {
		return ErrMissingConnectionHeader
	}
	rawHeader = strings.ToLower(strings.TrimSpace(rawHeader))
	iter := strings.SplitSeq(rawHeader, ",")

	for header := range iter {
		if strings.TrimSpace(header) == "upgrade" {
			return nil
		}
	}

	return ErrInvalidConnectionHeader
}

func validateUpgradeHeader(r *http.Request) error {
	rawHeader := r.Header.Get("Upgrade")
	if rawHeader == "" {
		return ErrMissingUpgradeHeader
	}
	rawHeader = strings.ToLower(strings.TrimSpace(rawHeader))
	iter := strings.SplitSeq(rawHeader, ",")

	for header := range iter {
		if strings.TrimSpace(header) == "websocket" {
			return nil
		}
	}

	return ErrInvalidUpgradeHeader
}

func validateVersionHeader(r *http.Request) error {
	header := r.Header.Get("Sec-WebSocket-Version")
	if header == "" {
		return ErrMissingVersionHeader
	} else if strings.TrimSpace(header) != "13" {
		return ErrInvalidVersionHeader
	}

	return nil
}

func validatedSecKeyHeader(r *http.Request) error {
	header := r.Header.Get("Sec-WebSocket-Key")
	header = strings.TrimSpace(header)
	if header == "" {
		return ErrMissingSecKey
	}

	decoded, err := base64.StdEncoding.DecodeString(header)
	if err != nil || len(decoded) != 16 {
		return ErrInvalidSecKey
	}

	return nil
}

func selectSubProtocol(r *http.Request, subProtocols []string) string {
	if subProtocols == nil {
		return ""
	}
	rawHeader := r.Header.Get("Sec-WebSocket-Protocol")
	if rawHeader == "" {
		return ""
	}

	rawHeader = strings.TrimSpace(rawHeader)
	iter := strings.SplitSeq(rawHeader, ",")

	for header := range iter {
		if slices.Contains(subProtocols, strings.TrimSpace(header)) {
			return strings.TrimSpace(header)
		}
	}

	return ""
}

// Appends the receive middleware to the middlewares slice of the upgrader.
func (u *Upgrader) Use(mw Middlware) {
	u.Middlwares = append(u.Middlwares, mw)
}

// This is from gorilla/websocket
type brNetConn struct {
	br *bufio.Reader
	net.Conn
}

// If there is still data in the http buffer it reads from it.
// When the http buffer gets empty, it sets it to nil to be collected by the GC,
// then for future reads it reads directly from the net.Conn.
func (b *brNetConn) Read(p []byte) (n int, err error) {
	if b.br != nil {
		// Limit read to buferred data.
		if n := b.br.Buffered(); len(p) > n {
			p = p[:n]
		}
		n, err = b.br.Read(p)
		if b.br.Buffered() == 0 {
			b.br = nil
		}
		return n, err
	}
	return b.Conn.Read(p)
}
