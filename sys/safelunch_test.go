//go:build windows

package sys

import (
	"strings"
	"testing"
)

func TestSafeLaunchScriptQuotesExecutableAndForwardsArguments(t *testing.T) {
	script := safeLaunchScript(`C:\机器人 50%\Glyc Cat.exe`)
	if !strings.Contains(script, `cd /d "%~dp0"`) ||
		!strings.Contains(script, `"%~dp0Glyc Cat.exe" %*`) ||
		strings.Contains(script, `C:\机器人 50%`) {
		t.Fatalf("unsafe launch script: %q", script)
	}
	percent := safeLaunchScript(`C:\bot\Glyc%Cat.exe`)
	if !strings.Contains(percent, `"%~dp0Glyc%%Cat.exe" %*`) {
		t.Fatalf("percent sign was not escaped: %q", percent)
	}
}
