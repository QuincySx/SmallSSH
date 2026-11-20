package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallReplacementLeavesNoOldBundle(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "first install", true: "replace"}[existing], func(t *testing.T) {
			dir := t.TempDir()
			stage, destination := filepath.Join(dir, "stage.app"), filepath.Join(dir, "SmallSSH.app")
			writeInstallFixture(t, stage, "new")
			if existing {
				writeInstallFixture(t, destination, "old")
			}
			if err := replaceInstalledBundle(stage, destination); err != nil {
				t.Fatal(err)
			}
			if b, err := os.ReadFile(filepath.Join(destination, "version")); err != nil || string(b) != "new" {
				t.Fatal("wrong installed bundle", err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 || entries[0].Name() != "SmallSSH.app" {
				t.Fatal("kept old bundle or staging files", entries, err)
			}
		})
	}
}

func TestInstallReplacementFailureRestoresOriginal(t *testing.T) {
	dir := t.TempDir()
	stage, destination := filepath.Join(dir, "missing.app"), filepath.Join(dir, "SmallSSH.app")
	writeInstallFixture(t, destination, "old")
	if err := replaceInstalledBundle(stage, destination); err == nil {
		t.Fatal("missing candidate succeeded")
	}
	if b, err := os.ReadFile(filepath.Join(destination, "version")); err != nil || string(b) != "old" {
		t.Fatal("lost original app", err)
	}
	if _, err := os.Lstat(stage + ".previous"); !os.IsNotExist(err) {
		t.Fatal("left a rollback copy", err)
	}
}

func TestInstallReplacementRejectsOccupiedRecoveryPath(t *testing.T) {
	dir := t.TempDir()
	stage, destination := filepath.Join(dir, "stage.app"), filepath.Join(dir, "SmallSSH.app")
	writeInstallFixture(t, stage, "new")
	writeInstallFixture(t, destination, "old")
	writeInstallFixture(t, stage+".previous", "unrelated")
	if err := replaceInstalledBundle(stage, destination); err == nil {
		t.Fatal("overwrote occupied recovery path")
	}
	for path, want := range map[string]string{stage: "new", destination: "old", stage + ".previous": "unrelated"} {
		if b, err := os.ReadFile(filepath.Join(path, "version")); err != nil || string(b) != want {
			t.Fatal("changed a bundle on refusal", path, err)
		}
	}
}

func writeInstallFixture(t *testing.T, path, version string) {
	t.Helper()
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "version"), []byte(version), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestInstallReusesOnlyIdenticalRegularExecutables(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "identical", true: "changed same size"}[changed], func(t *testing.T) {
			root := t.TempDir()
			old, stage := filepath.Join(root, "old.app"), filepath.Join(root, "stage.app")
			rel := filepath.Join("Contents", "Resources", "smallsshd")
			for bundle, contents := range map[string]string{old: "old", stage: map[bool]string{false: "old", true: "new"}[changed]} {
				path := filepath.Join(bundle, rel)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(contents), 0700); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Stat(filepath.Join(old, rel))
			if err != nil {
				t.Fatal(err)
			}
			if err := reuseInstalledExecutables(stage, old); err != nil {
				t.Fatal(err)
			}
			after, err := os.Stat(filepath.Join(stage, rel))
			if err != nil {
				t.Fatal(err)
			}
			if os.SameFile(before, after) == changed {
				t.Fatal("reused a different program or replaced an unchanged inode")
			}
			if err := reuseInstalledExecutables(stage, old); err != nil {
				t.Fatal("repeat reuse", err)
			}
		})
	}
	t.Run("symlink", func(t *testing.T) {
		root := t.TempDir()
		old, stage := filepath.Join(root, "old.app"), filepath.Join(root, "stage.app")
		rel := filepath.Join("Contents", "Resources", "smallsshd")
		for _, bundle := range []string{old, stage} {
			if err := os.MkdirAll(filepath.Dir(filepath.Join(bundle, rel)), 0700); err != nil {
				t.Fatal(err)
			}
		}
		target := filepath.Join(root, "outside")
		if err := os.WriteFile(target, []byte("old"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(old, rel)); err != nil {
			t.Skip("symlinks unavailable", err)
		}
		if err := os.WriteFile(filepath.Join(stage, rel), []byte("old"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := reuseInstalledExecutables(stage, old); err != nil {
			t.Fatal(err)
		}
		external, _ := os.Stat(target)
		candidate, err := os.Lstat(filepath.Join(stage, rel))
		if err != nil || !candidate.Mode().IsRegular() || os.SameFile(external, candidate) {
			t.Fatal("followed a linked old executable", err)
		}
	})
}
