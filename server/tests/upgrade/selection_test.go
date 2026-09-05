package upgrade

import (
	"fmt"
	"testing"
)

type upgradeCase struct {
	baseline baseline
	backend  string
	size     string
}

func selectCases(m manifest, scope, version, backend string) ([]upgradeCase, error) {
	if scope == "" {
		scope = "latest"
		if version != "" {
			scope = "full"
		}
	}
	if scope != "latest" && scope != "full" {
		return nil, fmt.Errorf("invalid UPGRADE_SCOPE %q", scope)
	}
	sizes := []string{"normal"}
	if scope == "full" {
		sizes = append(sizes, "large")
	}
	var cases []upgradeCase
	for _, b := range m.Baselines {
		if scope == "latest" && b.Version != m.Latest {
			continue
		}
		if version != "" && version != b.Version {
			continue
		}
		for _, db := range []string{"sqlite", "postgres"} {
			if backend != "" && backend != db {
				continue
			}
			for _, size := range sizes {
				cases = append(cases, upgradeCase{b, db, size})
			}
		}
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("upgrade filters selected no cases (scope=%q, baseline=%q, backend=%q)", scope, version, backend)
	}
	return cases, nil
}

func TestSelectCases(t *testing.T) {
	m := manifest{Latest: "v2.7.3", Baselines: []baseline{{Version: "v2.7.3"}, {Version: "v2.7.0"}}}
	for _, tc := range []struct {
		name, scope, version, backend string
		count                         int
		wantVersion                   string
	}{
		{name: "default latest", count: 2, wantVersion: "v2.7.3"},
		{name: "older filter implies full", version: "v2.7.0", count: 4, wantVersion: "v2.7.0"},
		{name: "older sqlite filter", version: "v2.7.0", backend: "sqlite", count: 2, wantVersion: "v2.7.0"},
		{name: "explicit scope conflict", scope: "latest", version: "v2.7.0"},
		{name: "full", scope: "full", count: 8},
		{name: "unknown scope", scope: "all"},
		{name: "unknown baseline", version: "v0.0.0"},
		{name: "unknown backend", backend: "mysql"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cases, err := selectCases(m, tc.scope, tc.version, tc.backend)
			if tc.count == 0 {
				if err == nil {
					t.Fatal("invalid selection accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(cases) != tc.count {
				t.Fatalf("selected %d cases, want %d", len(cases), tc.count)
			}
			seen := map[string]bool{}
			for _, c := range cases {
				if tc.wantVersion != "" && c.baseline.Version != tc.wantVersion {
					t.Fatalf("wrong baseline: %s", c.baseline.Version)
				}
				if tc.backend != "" && c.backend != tc.backend {
					t.Fatalf("wrong backend: %s", c.backend)
				}
				key := c.baseline.Version + "/" + c.backend + "/" + c.size
				if seen[key] {
					t.Fatalf("duplicate case %s", key)
				}
				seen[key] = true
			}
		})
	}
}
