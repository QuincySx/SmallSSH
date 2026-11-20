package vault

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestProfileCreationLimitsMatchAndPreserveExistingEntries(t *testing.T) {
	for _, count := range []int{MaxProfiles, MaxProfiles + 1} {
		for _, save := range []bool{false, true} {
			t.Run(fmt.Sprintf("count=%d/save=%t", count, save), func(t *testing.T) {
				s, err := New(testPass)
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				// Also represent a legacy vault created by the formerly uncapped Put.
				for i := range count {
					p := profile()
					p.ID = fmt.Sprint(i)
					s.profiles = append(s.profiles, p)
				}
				file := filepath.Join(t.TempDir(), "vault.json")
				write := s.Put
				if save {
					write = func(p Profile) error { return s.SaveProfile(file, p) }
				}
				p := profile()
				p.ID = "new-entry"
				if err := write(p); err == nil || len(s.profiles) != count {
					t.Fatal("creation exceeded the shared profile limit", err)
				}
				p.ID, p.Name = "0", "updated at capacity"
				if err := write(p); err != nil {
					t.Fatal("existing entries must remain editable", err)
				}
				stored, err := s.Profile(p.ID)
				if err != nil || stored.Name != p.Name || len(s.profiles) != count {
					t.Fatal("update at capacity lost existing data", err)
				}
			})
		}
	}
}
