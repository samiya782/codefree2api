package srd

import "testing"

func TestLatestVersionWalks(t *testing.T) {
	if got := LatestVersion([]byte(`{"data":{"latestVersion":"9.8.7"}}`)); got != "9.8.7" {
		t.Fatalf("got %q", got)
	}
	if got := LatestVersion([]byte(`{}`)); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestSpaceVersion(t *testing.T) {
	if got := SpaceVersion([]byte(`[{"version":"9.9.9"}]`)); got != "9.9.9" {
		t.Fatalf("version %q", got)
	}
	if got := SpaceVersion([]byte(`{"data":[{"artifactVersion":"1.5.1","name":"CodeFree"}]}`)); got != "1.5.1" {
		t.Fatalf("artifact %q", got)
	}
	if got := SpaceVersion([]byte(`{"name":"2.0.0"}`)); got != "2.0.0" {
		t.Fatalf("name %q", got)
	}
	if got := SpaceVersion([]byte(`{"nope":true}`)); got != "" {
		t.Fatalf("empty %q", got)
	}
}

func TestModelIDs(t *testing.T) {
	got := ModelIDs([]byte(`{"models":[{"id":"GLM-5.2"},{"name":"other"}]}`))
	if len(got) != 2 || got[0] != "GLM-5.2" || got[1] != "other" {
		t.Fatalf("%v", got)
	}
	if len(ModelIDs([]byte(`[]`))) != 0 {
		t.Fatal("empty")
	}
}

func TestProject(t *testing.T) {
	id, name := Project([]byte(`{"project":{"id":12,"name":"Demo"}}`))
	if id != 12 || name != "Demo" {
		t.Fatalf("%d %s", id, name)
	}
	id, name = Project([]byte(`{"code":1}`))
	if id != 0 || name != "" {
		t.Fatalf("missing %d %s", id, name)
	}
}
