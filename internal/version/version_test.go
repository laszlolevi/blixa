package version

import (
	"regexp"
	"testing"
)

func TestGetIsStableHash(t *testing.T) {
	v := Get()
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(v) {
		t.Fatalf("Get() = %q", v)
	}
	if Get() != v {
		t.Fatal("the version is not stable")
	}
}

func TestTagFrom(t *testing.T) {
	cases := []struct {
		version, rev string
		want         string
	}{
		{"v0.2.0", "78b80034ed03561dc9a91e06e59b3f10a192d2a5", "v0.2.0"},
		{"v0.2.0+dirty", "78b80034ed03561dc9a91e06e59b3f10a192d2a5", "v0.2.0+dirty"},
		{"v0.2.1-0.20261011103554-78b80034ed03", "78b80034ed03561dc9a91e06e59b3f10a192d2a5", "dev 78b8003"},
		{"v0.2.1-0.20261011103554-78b80034ed03+dirty", "78b80034ed03561dc9a91e06e59b3f10a192d2a5", "dev 78b8003+dirty"},
		{"v0.0.0-20261011103554-78b80034ed03", "78b80034ed03561dc9a91e06e59b3f10a192d2a5", "dev 78b8003"},
		{"(devel)", "", "dev"},
		{"", "", "dev"},
	}
	for _, c := range cases {
		if got := tagFrom(c.version, c.rev); got != c.want {
			t.Errorf("tagFrom(%q, %q) = %q, want %q", c.version, c.rev, got, c.want)
		}
	}
}
