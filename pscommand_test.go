package main

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestPsCommandEncodesUTF16LE(t *testing.T) {
	script := "Write-Output 'héllo 🪟'"
	got := strings.TrimPrefix(psCommand(script), "powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand ")
	u := utf16.Encode([]rune(script))
	want := make([]byte, 0, len(u)*2)
	for _, x := range u {
		want = append(want, byte(x), byte(x>>8))
	}
	if got != base64.StdEncoding.EncodeToString(want) {
		t.Fatalf("encoding differs:\n got %s\nwant %s", got, base64.StdEncoding.EncodeToString(want))
	}
}
