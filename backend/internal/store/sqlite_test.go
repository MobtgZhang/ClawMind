package store

import (
	"path/filepath"
	"testing"
)

func TestOpenSQLite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.DB() == nil {
		t.Fatal("nil db")
	}
}
