package value

import "testing"

func TestNormalizeWebSourceURI(t *testing.T) {
	got, err := NormalizeWebSourceURI("HTTPS://Example.COM:443?b=2&a=1#fragment")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.com/?b=2&a=1" {
		t.Fatalf("normalized URI = %q", got)
	}
}

func TestNormalizeWebSourceURIRejectsRelativeURL(t *testing.T) {
	if _, err := NormalizeWebSourceURI("/relative"); err == nil {
		t.Fatal("relative URL unexpectedly accepted")
	}
}
