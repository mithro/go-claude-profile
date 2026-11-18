package usage

import (
	"errors"
	"fmt"
	"testing"
)

func TestIs(t *testing.T) {
	u := Errorf("bad %s", "flag")
	if u.Error() != "bad flag" {
		t.Errorf("message = %q", u.Error())
	}
	if !Is(u) {
		t.Error("Is(usage error) = false")
	}
	if !Is(fmt.Errorf("wrapped: %w", u)) {
		t.Error("Is(wrapped usage error) = false")
	}
	if Is(errors.New("plain")) {
		t.Error("Is(plain error) = true")
	}
	if Is(nil) {
		t.Error("Is(nil) = true")
	}
}
