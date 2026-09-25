package image

import (
	"bytes"
	stdimage "image"
	"image/png"
	"testing"
)

func TestEncoderImageBoundsAndReaderPosition(t *testing.T) {
	var input bytes.Buffer
	if err := png.Encode(&input, stdimage.NewRGBA(stdimage.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	reader := bytes.NewReader(input.Bytes())
	if _, err := reader.Seek(1, 0); err != nil {
		t.Fatal(err)
	}
	if kind, ok := CheckImage(reader); !ok || kind != "image/png" {
		t.Fatalf("CheckImage() = %q, %v", kind, ok)
	}
	if position, err := reader.Seek(0, 1); err != nil || position != 0 {
		t.Fatalf("reader position = %d, %v", position, err)
	}
	output, err := EncoderImage(input.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(output)); err != nil {
		t.Fatalf("encoded image is invalid: %v", err)
	}
	if _, err := EncoderImage(make([]byte, maxImageBytes+1)); err == nil {
		t.Fatal("oversized input was accepted")
	}
	var oversized bytes.Buffer
	if err := png.Encode(&oversized, stdimage.NewRGBA(stdimage.Rect(0, 0, 8193, 1))); err != nil {
		t.Fatal(err)
	}
	if _, err := EncoderImage(oversized.Bytes()); err == nil {
		t.Fatal("oversized dimensions were accepted")
	}
}
