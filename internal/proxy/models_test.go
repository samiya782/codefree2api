package proxy

import (
	"reflect"
	"testing"
)

func TestParseModelLinesCodefreePrefix(t *testing.T) {
	in := "" +
		"Warning: The 'NO_COLOR' env is ignored\n" +
		"      at warnOnDeactivatedColors (internal:tty:33:24)\n" +
		"\n" +
		"codefree/GLM-5.1-AH\n" +
		"codefree/GLM-5.2\n" +
		"openai/gpt-4\n" +
		"  codefree/Qwen3.8-27B  \n" +
		"codefree/GLM-5.2\n" +
		"codefree/foo extra-column\n"
	got := ParseModelLines(in)
	want := []string{
		"codefree/GLM-5.1-AH",
		"codefree/GLM-5.2",
		"codefree/Qwen3.8-27B",
		"codefree/foo",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestParseModelLinesStripsANSI(t *testing.T) {
	in := "\x1b[32mcodefree/GLM-5.2\x1b[0m\ncodefree/Qwen3.8-27B"
	got := ParseModelLines(in)
	want := []string{"codefree/GLM-5.2", "codefree/Qwen3.8-27B"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestParseModelLinesEmpty(t *testing.T) {
	if got := ParseModelLines(""); got != nil && len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestStripANSI(t *testing.T) {
	got := StripANSI("\x1b[31mred\x1b[0m")
	if got != "red" {
		t.Fatalf("got %q", got)
	}
}
