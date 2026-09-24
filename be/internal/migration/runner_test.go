package migration

import (
	"testing"
	"testing/fstest"
)

func TestDiscoverOrdersMigrations(t *testing.T) {
	files := fstest.MapFS{
		"000010_later.up.sql": &fstest.MapFile{},
		"000001_init.up.sql":  &fstest.MapFile{},
	}

	migrations, err := discover(files)
	if err != nil {
		t.Fatalf("discover() error = %v", err)
	}
	if len(migrations) != 2 || migrations[0].Version != 1 || migrations[1].Version != 10 {
		t.Fatalf("discover() = %#v", migrations)
	}
}

func TestDiscoverRejectsDuplicateVersions(t *testing.T) {
	files := fstest.MapFS{
		"000001_init.up.sql":  &fstest.MapFile{},
		"000001_other.up.sql": &fstest.MapFile{},
	}

	if _, err := discover(files); err == nil {
		t.Fatal("discover() error = nil, want duplicate version error")
	}
}
