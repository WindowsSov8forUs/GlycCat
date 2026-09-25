package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/WindowsSov8forUs/glyccat/log"
	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

func TestSDKLoggerUsesApplicationLevel(t *testing.T) {
	var output bytes.Buffer
	appLogger := log.GetLogger()
	oldLevel := appLogger.Level
	appLogger.SetOutput(&output)
	defer func() {
		log.SetLogLevel(oldLevel)
		appLogger.SetOutput(os.Stdout)
	}()
	log.SetLogLevel(log.INFO)
	Logger{}.Log(context.Background(), server.LogLevelWarn, "sdk-warning")
	if !strings.Contains(output.String(), "sdk-warning") {
		t.Fatalf("SDK warning was not logged: %q", output.String())
	}
	output.Reset()
	log.SetLogLevel(log.OFF)
	Logger{}.Log(context.Background(), server.LogLevelWarn, "hidden")
	if output.Len() != 0 {
		t.Fatalf("OFF logged %q", output.String())
	}
}
