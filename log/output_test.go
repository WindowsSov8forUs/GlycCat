package log

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

func TestStartCreatesLogOnlyAfterCalledAndClosesIt(t *testing.T) {
	t.Chdir(t.TempDir())
	path := filepath.Join("log", "glyc-cat.log")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("log file already exists before Start: %v", err)
	}
	old := logger.Level
	defer func() {
		SetLogLevel(old)
		logger.SetOutput(os.Stdout)
	}()
	SetLogLevel(INFO)
	if err := Start(); err != nil {
		t.Fatal(err)
	}
	logger.Println(INFO, "file-output-test")
	if err := Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "file-output-test") || bytes.Contains(data, []byte("\x1b[")) {
		t.Fatalf("log file = %q, %v", data, err)
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
