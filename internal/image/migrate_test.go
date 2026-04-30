package image

import (
	"bytes"
	"image"
	imgcolor "image/color"
	imgpng "image/png"
	"os"
	"path/filepath"
	"testing"
)

func writePNG(t *testing.T, path string) {
	t.Helper()
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			src.Set(x, y, imgcolor.RGBA{1, 2, 3, 255})
		}
	}
	var buf bytes.Buffer
	imgpng.Encode(&buf, src)
	os.WriteFile(path, buf.Bytes(), 0600)
}

func TestMigrateAvatars_RenamesFiles(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	writePNG(t, filepath.Join(src, "U123.img"))
	writePNG(t, filepath.Join(src, "U456.img"))

	n, err := MigrateAvatars(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("migrated %d, want 2", n)
	}
	if _, err := os.Stat(filepath.Join(dst, "avatar-U123.png")); err != nil {
		t.Errorf("expected avatar-U123.png in dst: %v", err)
	}
	if _, err := os.Stat(filepath.Join(src, "U123.img")); !os.IsNotExist(err) {
		t.Errorf("expected source removed, err=%v", err)
	}
}

func TestMigrateAvatars_Idempotent(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writePNG(t, filepath.Join(src, "U1.img"))
	writePNG(t, filepath.Join(dst, "avatar-U1.png"))

	n, err := MigrateAvatars(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("migrated %d, want 0 (target existed)", n)
	}
	if _, err := os.Stat(filepath.Join(src, "U1.img")); !os.IsNotExist(err) {
		t.Errorf("expected source removed even when target existed")
	}
}

func TestMigrateAvatars_MissingSourceIsNoOp(t *testing.T) {
	dst := t.TempDir()
	n, err := MigrateAvatars(filepath.Join(t.TempDir(), "does-not-exist"), dst)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expected 0, got %d", n)
	}
}
