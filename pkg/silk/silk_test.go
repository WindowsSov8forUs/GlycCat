package silk

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestEncoderSilkContextProducesSilk(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is unavailable")
	}
	if _, err := getSilkCodecPath(); err != nil {
		t.Skipf("embedded codec is unavailable: %v", err)
	}
	inputPath := filepath.Join(t.TempDir(), "input.wav")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=0.2", "-ar", "24000", "-ac", "1", inputPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate input: %v: %s", err, output)
	}
	input, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	output, err := EncoderSilkContext(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if !IsAMRorSILK(output) {
		t.Fatal("transcoded output is not SILK")
	}
}
