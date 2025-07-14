package snapws

import (
	"crypto/sha1"
	"encoding/base64"
	"net/http"
	"slices"
	"strings"
)

const GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

func handShake(w http.ResponseWriter, r *http.Request, subProtocols []string, rejectRaw bool) (string, error) {
	if r.Method != http.MethodGet {
		return "", ErrWrongMethod
	}

	if err := validateUpgradeHeader(r); err != nil {
		return "", err
	}
	if err := validateConnectionHeader(r); err != nil {
		return "", err
	}
	if err := validateVersionHeader(r); err != nil {
		return "", err
	}
	if err := validatedSecKeyHeader(r); err != nil {
		return "", err
	}

	subProtocol := ChooseSubProtocol(r, subProtocols)
	if subProtocol == "" && rejectRaw {
		return "", ErrNotSupportedSubProtocols
	}

	hashedKey := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + GUID))
	secAcceptKey := base64.StdEncoding.EncodeToString(hashedKey[:])

	if subProtocol != "" {
		w.Header().Set("Sec-WebSocket-Protocol", subProtocol)
	}
	w.Header().Set("Upgrade", "websocket")
	w.Header().Set("Connection", "Upgrade")
	w.Header().Set("Sec-WebSocket-Accept", secAcceptKey)
	w.WriteHeader(http.StatusSwitchingProtocols)

	return subProtocol, nil
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

func ChooseSubProtocol(r *http.Request, subProtocols []string) string {
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
