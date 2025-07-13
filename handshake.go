package snapws

import (
	"crypto/sha1"
	"encoding/base64"
	"net/http"
	"strings"
)

const GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

func handShake(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		return ErrWrongMethod
	}

	if err := validateUpgradeHeader(r); err != nil {
		return err
	}
	if err := validateConnectionHeader(r); err != nil {
		return err
	}
	if err := validateVersionHeader(r); err != nil {
		return err
	}
	if err := validatedSecKeyHeader(r); err != nil {
		return err
	}

	hashedKey := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + GUID))
	secAcceptKey := base64.StdEncoding.EncodeToString(hashedKey[:])

	w.Header().Set("Upgrade", "websocket")
	w.Header().Set("Connection", "Upgrade")
	w.Header().Set("Sec-WebSocket-Accept", secAcceptKey)
	w.WriteHeader(http.StatusSwitchingProtocols)

	return nil
}

func validateConnectionHeader(r *http.Request) error {
	header := r.Header.Get("Connection")
	if header == "" {
		return ErrMissingConnectionHeader
	} else if !strings.Contains(strings.ToLower(header), "upgrade") {
		return ErrInvalidConnectionHeader
	}

	return nil
}

func validateUpgradeHeader(r *http.Request) error {
	header := r.Header.Get("Upgrade")
	if header == "" {
		return ErrMissingUpgradeHeader
	} else if !strings.EqualFold(header, "websocket") {
		return ErrInvalidUpgradeHeader
	}

	return nil
}
func validateVersionHeader(r *http.Request) error {
	header := r.Header.Get("Sec-WebSocket-Version")
	if header == "" {
		return ErrMissingVersionHeader
	} else if header != "13" {
		return ErrInvalidVersionHeader
	}

	return nil
}

func validatedSecKeyHeader(r *http.Request) error {
	header := r.Header.Get("Sec-WebSocket-Key")
	if header == "" {
		return ErrMissingSecKey
	}

	decoded, err := base64.StdEncoding.DecodeString(header)
	if err != nil || len(decoded) != 16 {
		return ErrInvalidSecKey
	}

	return nil
}
