package cli

import (
	"os"
	"testing"
)

func TestDevNullIsNotATTY(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if isTTY(f) {
		t.Fatal("/dev/null must not count as a terminal")
	}
}
