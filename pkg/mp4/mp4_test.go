package mp4

import (
	"encoding/binary"
	"testing"
)

func TestIsMP4ReadsFileTypeBox(t *testing.T) {
	makeBox := func(major, minor, compatible string) []byte {
		box := make([]byte, 20)
		binary.BigEndian.PutUint32(box[:4], uint32(len(box)))
		copy(box[4:8], "ftyp")
		copy(box[8:12], major)
		copy(box[12:16], minor)
		copy(box[16:20], compatible)
		return box
	}
	for _, tc := range []struct {
		name string
		data []byte
		want bool
	}{
		{"major brand", makeBox("mp42", "0000", "xxxx"), true},
		{"compatible brand", makeBox("xxxx", "0000", "isom"), true},
		{"minor version is not a brand", makeBox("xxxx", "isom", "xxxx"), false},
		{"wrong box type", append([]byte{0, 0, 0, 20, 'm', 'o', 'o', 'v'}, makeBox("mp42", "0000", "xxxx")[8:]...), false},
		{"truncated box", makeBox("mp42", "0000", "xxxx")[:12], false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsMP4(tc.data); got != tc.want {
				t.Fatalf("IsMP4() = %v, want %v", got, tc.want)
			}
		})
	}
}
