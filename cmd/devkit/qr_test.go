package main

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestQRCodesKeepTheirVerifiedShape(t *testing.T) {
	// These two were decoded with zbarimg when the encoder was written; any change to the output
	// needs a new check with a real decoder.
	text := "https://t3.dev.example.com/pair#token=" + strings.Repeat("aB3-_x9Z", 30)
	for n, want := range map[int]string{
		45:  "4c8e0989ba2256616b1f6f1e62c4a39d34c4907704d4f2d5bef6148181afd153",
		150: "16fc3a0de3f17ebea55ba54d6d44621ec2891f74fd07fda86b046ef2849927ab",
	} {
		m, err := qrCode(text[:n])
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256([]byte(qrText(m)))); got != want {
			t.Errorf("%d bytes: QR code changed (%s)", n, got)
		}
	}
}

func TestQRCodesPickTheSmallestVersionAndRefuseTooMuch(t *testing.T) {
	for _, c := range []struct{ bytes, version int }{{1, 1}, {14, 1}, {15, 2}, {120, 7}, {213, 10}} {
		m, err := qrCode(strings.Repeat("x", c.bytes))
		if err != nil || len(m) != 4*c.version+17 {
			t.Errorf("%d bytes: size %d, %v; want version %d", c.bytes, len(m), err, c.version)
		}
	}
	if _, err := qrCode(strings.Repeat("x", 214)); err == nil {
		t.Error("214 bytes should not fit")
	}
	m, _ := qrCode("x")
	for _, corner := range [][2]int{{0, 0}, {14, 0}, {0, 14}} { // finder patterns' dark centres
		if !m[corner[1]+3][corner[0]+3] {
			t.Errorf("no finder at %v", corner)
		}
	}
}
