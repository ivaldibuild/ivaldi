package env

import (
	"reflect"
	"testing"
	"time"
)

func newParser() *parser {
	return &parser{opts: Options{SliceSeparator: ","}.withDefaults()}
}

func TestSetValue_String(t *testing.T) {
	p := newParser()
	var s string
	v := reflect.ValueOf(&s).Elem()
	if err := p.setValue(v, "hello", "Field", "ENV"); err != nil {
		t.Fatal(err)
	}
	if s != "hello" {
		t.Errorf("expected hello, got %q", s)
	}
}

func TestSetValue_Int(t *testing.T) {
	p := newParser()
	var n int
	v := reflect.ValueOf(&n).Elem()
	if err := p.setValue(v, "42", "Field", "ENV"); err != nil {
		t.Fatal(err)
	}
	if n != 42 {
		t.Errorf("expected 42, got %d", n)
	}
}

func TestSetValue_Int_Invalid(t *testing.T) {
	p := newParser()
	var n int
	v := reflect.ValueOf(&n).Elem()
	err := p.setValue(v, "abc", "Field", "ENV")
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := err.(*ParseError); !ok {
		t.Errorf("expected ParseError, got %T", err)
	}
}

func TestSetValue_Bool(t *testing.T) {
	p := newParser()
	tests := []struct {
		input string
		want  bool
	}{
		{"true", true},
		{"false", false},
		{"1", true},
		{"0", false},
		{"TRUE", true},
		{"t", true},
		{"f", false},
	}
	for _, tt := range tests {
		var b bool
		v := reflect.ValueOf(&b).Elem()
		if err := p.setValue(v, tt.input, "Field", "ENV"); err != nil {
			t.Errorf("input %q: %v", tt.input, err)
			continue
		}
		if b != tt.want {
			t.Errorf("input %q: expected %v, got %v", tt.input, tt.want, b)
		}
	}
}

func TestSetValue_Float64(t *testing.T) {
	p := newParser()
	var f float64
	v := reflect.ValueOf(&f).Elem()
	if err := p.setValue(v, "3.14", "Field", "ENV"); err != nil {
		t.Fatal(err)
	}
	if f != 3.14 {
		t.Errorf("expected 3.14, got %f", f)
	}
}

func TestSetValue_Duration(t *testing.T) {
	p := newParser()
	var d time.Duration
	v := reflect.ValueOf(&d).Elem()
	if err := p.setValue(v, "5m30s", "Field", "ENV"); err != nil {
		t.Fatal(err)
	}
	expected := 5*time.Minute + 30*time.Second
	if d != expected {
		t.Errorf("expected %v, got %v", expected, d)
	}
}

func TestSetValue_Duration_Invalid(t *testing.T) {
	p := newParser()
	var d time.Duration
	v := reflect.ValueOf(&d).Elem()
	err := p.setValue(v, "abc", "Field", "ENV")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSetValue_Uint(t *testing.T) {
	p := newParser()
	var n uint
	v := reflect.ValueOf(&n).Elem()
	if err := p.setValue(v, "100", "Field", "ENV"); err != nil {
		t.Fatal(err)
	}
	if n != 100 {
		t.Errorf("expected 100, got %d", n)
	}
}

func TestSetSlice_Strings(t *testing.T) {
	p := newParser()
	var s []string
	v := reflect.ValueOf(&s).Elem()
	if err := p.setSlice(v, "a,b,c", "Field", "ENV"); err != nil {
		t.Fatal(err)
	}
	if len(s) != 3 || s[0] != "a" || s[1] != "b" || s[2] != "c" {
		t.Errorf("expected [a b c], got %v", s)
	}
}

func TestSetSlice_TrimSpaces(t *testing.T) {
	p := newParser()
	var s []string
	v := reflect.ValueOf(&s).Elem()
	if err := p.setSlice(v, "a , b , c", "Field", "ENV"); err != nil {
		t.Fatal(err)
	}
	if len(s) != 3 || s[0] != "a" || s[1] != "b" || s[2] != "c" {
		t.Errorf("expected [a b c], got %v", s)
	}
}

func TestSetSlice_Empty(t *testing.T) {
	p := newParser()
	var s []string
	v := reflect.ValueOf(&s).Elem()
	if err := p.setSlice(v, "", "Field", "ENV"); err != nil {
		t.Fatal(err)
	}
	if len(s) != 0 {
		t.Errorf("expected empty slice, got %v", s)
	}
}

func TestSetSlice_Ints(t *testing.T) {
	p := newParser()
	var s []int
	v := reflect.ValueOf(&s).Elem()
	if err := p.setSlice(v, "1,2,3", "Field", "ENV"); err != nil {
		t.Fatal(err)
	}
	if len(s) != 3 || s[0] != 1 || s[1] != 2 || s[2] != 3 {
		t.Errorf("expected [1 2 3], got %v", s)
	}
}

func TestSetValue_NamedStringType(t *testing.T) {
	type MyType string
	p := newParser()
	var v MyType
	rv := reflect.ValueOf(&v).Elem()
	if err := p.setValue(rv, "custom", "Field", "ENV"); err != nil {
		t.Fatal(err)
	}
	if v != "custom" {
		t.Errorf("expected custom, got %q", v)
	}
}
