package snapws

import (
	"crypto/sha1"
	"encoding/base64"
	"net"
	"net/http"
	"slices"
	"strings"
)

const GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

func (m *Manager[KeyType]) handShake(w http.ResponseWriter, r *http.Request) (net.Conn, string, error) {
	// validate method and headers
	if r.Method != http.MethodGet {
		http.Error(w, "request method should be get", http.StatusMethodNotAllowed)
		return nil, "", ErrWrongMethod
	}

	if err := validateUpgradeHeader(r); err != nil {
		http.Error(w, "invalid or missing upgrade header", http.StatusUpgradeRequired)
		return nil, "", err
	}
	if err := validateConnectionHeader(r); err != nil {
		http.Error(w, "invalid or missing connection header", http.StatusBadRequest)
		return nil, "", err
	}
	if err := validateVersionHeader(r); err != nil {
		http.Error(w, "invalid or missing Sec-WebSocket-Version header, must be 13", http.StatusBadRequest)
		return nil, "", err
	}
	if err := validatedSecKeyHeader(r); err != nil {
		http.Error(w, "invalid or missing Sec-WebSocket-Key header", http.StatusBadRequest)
		return nil, "", err
	}

	// selecting a subprotocol
	subProtocol := selectSubProtocol(r, m.SubProtocols)
	if subProtocol == "" && m.RejectRaw {
		http.Error(w, "unsupported or missing subprotocol", http.StatusBadRequest)
		return nil, "", ErrNotSupportedSubProtocols
	}

	// running middlewares
	if m.Middlwares != nil {
		for _, middleware := range m.Middlwares {
			if err := middleware(w, r); err != nil {
				if mwErr, ok := AsMiddlewareErr(err); ok {
					http.Error(w, mwErr.Message, mwErr.Code)
				}
				http.Error(w, "middleware error", http.StatusBadRequest)
				return nil, "", err
			}
		}
	}

	// computing key
	hashedKey := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + GUID))
	secAcceptKey := base64.StdEncoding.EncodeToString(hashedKey[:])

	// hijacking writer
	c, brw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		http.Error(w, "failed to hijack connection", http.StatusInternalServerError)
		return nil, "", err
	}

	// writing response
	p := brw.Writer.AvailableBuffer()
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

	return c, subProtocol, err
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
