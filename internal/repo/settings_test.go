package repo

import "testing"

func TestPlatformSettingRoundTrip(t *testing.T) {
	var nilSettings *SettingsRepo
	if got := nilSettings.PlatformSetting("missing"); got != "" {
		t.Fatalf("nil receiver = %q", got)
	}
	s, err := OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.PlatformSetting(" missing "); got != "" {
		t.Fatalf("missing setting = %q", got)
	}
	if err := s.SetPlatformSetting("   ", "ignored"); err != nil {
		t.Fatal(err)
	}
	if got := s.PlatformSetting(""); got != "" {
		t.Fatalf("blank name stored = %q", got)
	}
	if err := s.SetPlatformSetting(" feature ", "first"); err != nil {
		t.Fatal(err)
	}
	if got := s.PlatformSetting("feature"); got != "first" {
		t.Fatalf("setting = %q", got)
	}
	if err := s.SetPlatformSetting("feature", "second"); err != nil {
		t.Fatal(err)
	}
	if got := s.PlatformSetting("feature"); got != "second" {
		t.Fatalf("overwritten setting = %q", got)
	}
	if err := s.SetPlatformSetting("blank", ""); err != nil {
		t.Fatal(err)
	}
	if got := s.PlatformSetting("blank"); got != "" {
		t.Fatalf("cleared setting = %q", got)
	}
}
