package main

import "testing"

func TestGoType(t *testing.T) {
	cases := []struct {
		name string
		f    FieldSchema
		nest string
		want string
	}{
		{"string", FieldSchema{Type: "String"}, "", "string"},
		{"string[]", FieldSchema{Type: "String[]"}, "", "[]string"},
		{"int", FieldSchema{Type: "Integer"}, "", "int"},
		{"unsigned int", FieldSchema{Type: "Unsigned Integer"}, "", "int"},
		{"int64", FieldSchema{Type: "Int64"}, "", "int64"},
		{"float", FieldSchema{Type: "Float"}, "", "float64"},
		{"bool", FieldSchema{Type: "Boolean"}, "", "bool"},
		{"object named", FieldSchema{Type: "Object"}, "User", "User"},
		{"object anon", FieldSchema{Type: "Object"}, "", "any"},
		{"object[] named", FieldSchema{Type: "Object[]"}, "User", "[]User"},
		{"map<string,string>", FieldSchema{Type: "map[string,string]"}, "", "map[string]string"},
		{"map<string,object>", FieldSchema{Type: "map[string]Object"}, "Metadata", "map[string]Metadata"},
		{"timestamp by name", FieldSchema{Type: "String", Name: "created_at"}, "", "time.Time"},
		{"timestamp by desc", FieldSchema{Type: "String", Description: "An RFC3339 time"}, "", "time.Time"},
		{"nullable", FieldSchema{Type: "String", Description: "Can be **null**"}, "", "*string"},
		// Lowercase variants (EventSub docs). Parser must be case-insensitive.
		{"lowercase string", FieldSchema{Type: "string"}, "", "string"},
		{"lowercase integer", FieldSchema{Type: "integer"}, "", "int"},
		{"lowercase boolean", FieldSchema{Type: "boolean"}, "", "bool"},
		{"lowercase object named", FieldSchema{Type: "object"}, "User", "User"},
		{"lowercase object[] named", FieldSchema{Type: "object[]"}, "Segment", "[]Segment"},
		{"lowercase timestamp", FieldSchema{Type: "string", Name: "created_at"}, "", "time.Time"},
	}
	for _, c := range cases {
		got := GoType(c.f, c.nest)
		if got != c.want {
			t.Errorf("%s: GoType(%+v, %q) = %q; want %q", c.name, c.f, c.nest, got, c.want)
		}
	}
}
