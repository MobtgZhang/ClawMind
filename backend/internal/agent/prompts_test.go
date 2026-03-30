package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadPromptFile_missingUsesDefault(t *testing.T) {
	dir := t.TempDir()
	got := readPromptFile(dir, promptFileSystemSuffix, defaultSystemSuffix)
	if got != defaultSystemSuffix {
		t.Fatalf("got %q want default", got)
	}
}

func TestReadPromptFile_customFile(t *testing.T) {
	dir := t.TempDir()
	custom := "custom suffix line"
	if err := os.WriteFile(filepath.Join(dir, promptFileSystemSuffix), []byte("  "+custom+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := readPromptFile(dir, promptFileSystemSuffix, defaultSystemSuffix)
	if got != custom {
		t.Fatalf("got %q want %q", got, custom)
	}
}

func TestExpandLastUser(t *testing.T) {
	tpl := "x " + LastUserPlaceholder + " y"
	if expandLastUser(tpl, "u") != "x u y" {
		t.Fatal(expandLastUser(tpl, "u"))
	}
}
