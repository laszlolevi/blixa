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
