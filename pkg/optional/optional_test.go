package optional

import "testing"

func TestOf(t *testing.T) {
	t.Run("the zero value is not set", func(t *testing.T) {
		if got := Of(""); got != nil {
			t.Fatalf("Of(\"\") = %q, want nil", *got)
		}
		if got := Of(0); got != nil {
			t.Fatalf("Of(0) = %d, want nil", *got)
		}
	})
	t.Run("any other value is pointed to", func(t *testing.T) {
		got := Of("prn_ada")
		if got == nil || *got != "prn_ada" {
			t.Fatalf("Of(\"prn_ada\") = %v, want a pointer to prn_ada", got)
		}
	})
	t.Run("each call returns its own copy", func(t *testing.T) {
		s := "a"
		got := Of(s)
		s = "b"
		if *got != "a" {
			t.Fatalf("the pointer follows the caller's variable: %q", *got)
		}
	})
}

func TestOfPtr(t *testing.T) {
	t.Run("nil and the zero value are not set", func(t *testing.T) {
		if got := OfPtr[string](nil); got != nil {
			t.Fatalf("OfPtr(nil) = %q, want nil", *got)
		}
		empty := ""
		if got := OfPtr(&empty); got != nil {
			t.Fatalf("OfPtr(&\"\") = %q, want nil", *got)
		}
	})
	t.Run("any other value is copied", func(t *testing.T) {
		s := "a"
		got := OfPtr(&s)
		if got == nil || got == &s {
			t.Fatalf("OfPtr(&s) = %v, want a new pointer", got)
		}
		s = "b"
		if *got != "a" {
			t.Fatalf("the copy follows the caller's variable: %q", *got)
		}
	})
}
