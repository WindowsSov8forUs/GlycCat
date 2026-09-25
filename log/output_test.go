package log

import (
	"bytes"
	"os"
	"testing"
)

func TestPlainWriterStripsTerminalColors(t *testing.T) {
	input := []byte("\x1b[31merror\x1b[0m\n")
	var output bytes.Buffer
	n, err := (plainWriter{writer: &output}).Write(input)
	if err != nil || n != len(input) || output.String() != "error\n" {
		t.Fatalf("plainWriter.Write() = %d, %v, %q", n, err, output.String())
	}
}

func TestOffLevelSuppressesMessages(t *testing.T) {
	old := logger.Level
	defer func() {
		SetLogLevel(old)
		logger.SetOutput(os.Stdout)
	}()
	var output bytes.Buffer
	logger.SetOutput(&output)
	SetLogLevel(OFF)
	logger.Println(INFO, "hidden")
	if output.Len() != 0 {
		t.Fatalf("OFF emitted %q", output.String())
	}
}
