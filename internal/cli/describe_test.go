package cli

import (
	"encoding/json"
	"regexp"
	"testing"
)

func TestDescribeCoversEveryFlagTheParserTakes(t *testing.T) {
	sample := func(f Flag) string {
		switch {
		case len(f.Values) > 0:
			return f.Values[0]
		case f.Default != "":
			return f.Default
		}
		return "1"
	}
	for _, f := range surface.Flags {
		for _, name := range append([]string{f.Name}, f.Aliases...) {
			argv := []string{"--" + name}
			if f.Kind != "bool" {
				argv = append(argv, sample(f))
			}
			o, err := Parse(argv)
			if err != nil {
				t.Errorf("--%s: %v", name, err)
				continue
			}
			if len(o.Positional) != 0 {
				t.Errorf("--%s was not consumed by Parse (positional %v)", name, o.Positional)
			}
		}
	}
}

func TestDescribeCoversEveryFlagInHelp(t *testing.T) {
	described := map[string]bool{}
	for _, f := range surface.Flags {
		described[f.Name] = true
		for _, a := range f.Aliases {
			described[a] = true
		}
	}
	for name := range harnessFlags {
		described[name] = true
	}
	re := regexp.MustCompile(`--[a-z0-9-]+`)
	for _, tok := range re.FindAllString(Help(), -1) {
		if !described[tok[2:]] {
			t.Errorf("%s appears in --help but has no --describe entry", tok)
		}
	}
}

func TestDescribeShape(t *testing.T) {
	s, err := Describe()
	if err != nil {
		t.Fatal(err)
	}
	var got Surface
	if err := json.Unmarshal([]byte(s), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != Version || got.ExitCodes["interrupted"] != 130 {
		t.Errorf("header = %q %v", got.Version, got.ExitCodes)
	}
	if len(got.Flags) != len(surface.Flags) {
		t.Fatalf("got %d flags, want %d", len(got.Flags), len(surface.Flags))
	}
	seen := map[string]bool{}
	for _, f := range got.Flags {
		if seen[f.Name] {
			t.Errorf("duplicate flag %q", f.Name)
		}
		seen[f.Name] = true
		switch f.Kind {
		case "bool", "number", "enum", "text", "path":
		default:
			t.Errorf("%s: unknown kind %q", f.Name, f.Kind)
		}
		if f.Kind == "enum" && len(f.Values) < 2 {
			t.Errorf("%s: enum without values", f.Name)
		}
		if f.Default != "" && len(f.Values) > 0 && !contains(f.Values, f.Default) {
			t.Errorf("%s: default %q not in %v", f.Name, f.Default, f.Values)
		}
		if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
			t.Errorf("%s: min %v > max %v", f.Name, *f.Min, *f.Max)
		}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestDescribeFlag(t *testing.T) {
	o, err := Parse([]string{"--describe"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.Describe || len(o.Positional) != 0 {
		t.Errorf("describe = %v positional %v", o.Describe, o.Positional)
	}
}
