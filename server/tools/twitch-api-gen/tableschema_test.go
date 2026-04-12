package main

import "testing"

func TestNestingDepth(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"id", 0},
		{"\u00a0\u00a0\u00a0id", 1},
		{"\u00a0\u00a0\u00a0\u00a0\u00a0\u00a0id", 2},
		{"   id", 1},
		{"\u00a0 \u00a0id", 1},
		{"", 0},
	}
	for _, c := range cases {
		got, err := nestingDepth(c.in)
		if err != nil {
			t.Errorf("nestingDepth(%q) unexpected err: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("nestingDepth(%q) = %d; want %d", c.in, got, c.want)
		}
	}
}

func TestSplitEnumValue(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"webhook", "webhook"},
		{"webhook \u2014 the webhook kind", "webhook"},
		{"webhook: the webhook kind", "webhook"},
		{"webhook - the webhook kind", "webhook"},
		{`""`, `""`},
	}
	for _, c := range cases {
		got := splitEnumValue(c.in)
		if got != c.want {
			t.Errorf("splitEnumValue(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}
