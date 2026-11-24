package lines

import "testing"

func TestParseArgs(t *testing.T) {
	got, err := parseArgs([]string{"8", "3"})
	if err != nil || got.accepted != 8 || got.effective != 3 || got.explicit || got.client != "" {
		t.Fatalf("manual %+v %v", got, err)
	}
	got, err = parseArgs([]string{"--remote", "https://example.test/r", "8", "3"})
	if err != nil || got.accepted != 8 || got.effective != 3 || len(got.remotes) != 1 || got.remotes[0] != "https://example.test/r" {
		t.Fatalf("remote %+v %v", got, err)
	}
	got, err = parseArgs([]string{"plugin", "8", "3"})
	if err != nil || got.client != "plugin" || got.accepted != 8 || got.effective != 3 {
		t.Fatalf("plugin %+v %v", got, err)
	}
	got, err = parseArgs([]string{"space", "spec", "8", "3"})
	if err != nil || got.client != "space" || !got.spaceSpec || got.accepted != 8 || got.effective != 3 {
		t.Fatalf("spec %+v %v", got, err)
	}
	got, err = parseArgs([]string{"space", "8", "3"})
	if err != nil || got.client != "space" || got.spaceSpec {
		t.Fatalf("space %+v %v", got, err)
	}
	for _, args := range [][]string{nil, {"1"}, {"1", "2", "3"}, {"-1", "2"}, {"1.5", "2"}, {"", "1"}, {"+1", "2"}, {"--remote"}, {"--port", "1"}, {"--commit", "HEAD", "1", "2"}, {"8", "plugin", "3"}, {"--plugin-version", "1.0.0", "8", "3"}, {"spec", "8", "3"}, {"plugin", "spec", "8", "3"}, {"space", "spec", "spec", "8", "3"}} {
		if _, err := parseArgs(args); err == nil {
			t.Fatalf("accepted %q", args)
		}
	}
}
