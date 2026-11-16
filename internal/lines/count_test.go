package lines

import "testing"

func TestParseArgs(t *testing.T) {
	accepted, effective, remotes, err := parseArgs([]string{"8", "3"})
	if err != nil || accepted != 8 || effective != 3 || remotes != nil {
		t.Fatalf("manual %d %d %#v %v", accepted, effective, remotes, err)
	}
	accepted, effective, remotes, err = parseArgs([]string{"--remote", "https://example.test/r", "8", "3"})
	if err != nil || accepted != 8 || effective != 3 || len(remotes) != 1 || remotes[0] != "https://example.test/r" {
		t.Fatalf("remote %d %d %#v %v", accepted, effective, remotes, err)
	}
	for _, args := range [][]string{nil, {"1"}, {"1", "2", "3"}, {"-1", "2"}, {"1.5", "2"}, {"", "1"}, {"+1", "2"}, {"--remote"}, {"--port", "1"}, {"--commit", "HEAD", "1", "2"}} {
		if _, _, _, err := parseArgs(args); err == nil {
			t.Fatalf("accepted %q", args)
		}
	}
}
