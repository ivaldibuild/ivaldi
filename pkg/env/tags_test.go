package env

import (
	"reflect"
	"testing"
)

func TestParseTag_Simple(t *testing.T) {
	tag := reflect.StructTag(`env:"MY_VAR"`)
	info, ok := parseTag(tag, "env")
	if !ok {
		t.Fatal("expected tag to be found")
	}
	if info.Name != "MY_VAR" {
		t.Errorf("expected Name=MY_VAR, got %q", info.Name)
	}
	if info.Required || info.NotEmpty || info.Expand {
		t.Error("expected no options set")
	}
}

func TestParseTag_Required(t *testing.T) {
	tag := reflect.StructTag(`env:"MY_VAR,required"`)
	info, ok := parseTag(tag, "env")
	if !ok {
		t.Fatal("expected tag to be found")
	}
	if info.Name != "MY_VAR" {
		t.Errorf("expected Name=MY_VAR, got %q", info.Name)
	}
	if !info.Required {
		t.Error("expected Required=true")
	}
}

func TestParseTag_MultipleOptions(t *testing.T) {
	tag := reflect.StructTag(`env:"MY_VAR,required,notEmpty,expand"`)
	info, ok := parseTag(tag, "env")
	if !ok {
		t.Fatal("expected tag to be found")
	}
	if info.Name != "MY_VAR" {
		t.Errorf("expected Name=MY_VAR, got %q", info.Name)
	}
	if !info.Required {
		t.Error("expected Required=true")
	}
	if !info.NotEmpty {
		t.Error("expected NotEmpty=true")
	}
	if !info.Expand {
		t.Error("expected Expand=true")
	}
}

func TestParseTag_NoTag(t *testing.T) {
	tag := reflect.StructTag(`yaml:"field"`)
	_, ok := parseTag(tag, "env")
	if ok {
		t.Error("expected tag not found")
	}
}

func TestParseTag_EmptyName(t *testing.T) {
	tag := reflect.StructTag(`env:",required"`)
	info, ok := parseTag(tag, "env")
	if !ok {
		t.Fatal("expected tag to be found")
	}
	if info.Name != "" {
		t.Errorf("expected empty Name, got %q", info.Name)
	}
	if !info.Required {
		t.Error("expected Required=true")
	}
}

func TestParseTag_CustomTagName(t *testing.T) {
	tag := reflect.StructTag(`config:"MY_VAR,required"`)
	info, ok := parseTag(tag, "config")
	if !ok {
		t.Fatal("expected tag to be found")
	}
	if info.Name != "MY_VAR" {
		t.Errorf("expected Name=MY_VAR, got %q", info.Name)
	}
	if !info.Required {
		t.Error("expected Required=true")
	}
}
