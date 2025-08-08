package snapws

import (
	"testing"
)

func TestMask(t *testing.T) {
	message := []byte("hello world")
	key := []byte{1, 2, 3, 4}

	mask(message, key, 0)
	if string(message) == "hello world" {
		t.Errorf("didnt mask")
	}

	mask(message, key, 0)
	if string(message) != "hello world" {
		t.Errorf("wrong unmask")
	}
}
