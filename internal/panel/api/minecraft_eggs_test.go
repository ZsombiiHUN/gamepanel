package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// The bundled Minecraft eggs must import cleanly and their defaults must pass
// their own rules, or a server created from them could not be saved.
func TestBundledMinecraftEggs(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("../../../eggs/minecraft/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no eggs found (%v)", err)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			e, err := importPTDL(raw)
			if err != nil {
				t.Fatal(err)
			}
			if e.Startup != "gp-java" {
				t.Errorf("startup %q", e.Startup)
			}
			for _, img := range append(e.DockerImages, store.EggImage{Image: e.InstallImage}) {
				if !strings.HasPrefix(img.Image, "ghcr.io/zsombiihun/gamepanel-java:") {
					t.Errorf("image %q", img.Image)
				}
			}
			if !strings.Contains(e.InstallScript, "exec gp-install ") {
				t.Errorf("install script %q", e.InstallScript)
			}
			profile := false
			for _, v := range e.Variables {
				if err := store.ValidateVar(v, v.Default); err != nil {
					t.Errorf("default of %s: %v", v.Env, err)
				}
				if v.Env == "GP_PROFILE" {
					profile = true
					if v.UserEditable || v.UserViewable {
						t.Error("GP_PROFILE must be hidden")
					}
				}
			}
			if !profile {
				t.Error("no GP_PROFILE variable")
			}
		})
	}
}
