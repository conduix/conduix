package handlers

import "testing"

func TestValidateConfigSchema(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"empty is ok (nullable)", "", false},
		{"valid minimal", `{"type":"geocode","fields":[]}`, false},
		{
			"valid with known field types",
			`{"type":"geocode","display_name":"Geocode","fields":[
				{"name":"address_field","type":"string","required":true},
				{"name":"api_key","type":"secret"},
				{"name":"rps","type":"number","default":20}
			]}`,
			false,
		},
		{
			"nested object field valid",
			`{"type":"x","fields":[{"name":"cache","type":"object","fields":[
				{"name":"dsn","type":"secret"}]}]}`,
			false,
		},
		{"not json", `{not-json`, true},
		{"missing type", `{"fields":[]}`, true},
		{"unknown field type", `{"type":"x","fields":[{"name":"f","type":"bogus"}]}`, true},
		{"field missing name", `{"type":"x","fields":[{"type":"string"}]}`, true},
		{"nested unknown type", `{"type":"x","fields":[{"name":"o","type":"object","fields":[{"name":"b","type":"nope"}]}]}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfigSchema(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConfigSchema(%q) err=%v, wantErr=%v", tt.raw, err, tt.wantErr)
			}
		})
	}
}

func TestParsePluginSchema(t *testing.T) {
	if _, ok := parsePluginSchema(""); ok {
		t.Error("empty should return ok=false")
	}
	if _, ok := parsePluginSchema("{bad"); ok {
		t.Error("invalid json should return ok=false")
	}
	schema, ok := parsePluginSchema(`{"type":"geocode","display_name":"Geo"}`)
	if !ok || schema.Type != "geocode" || schema.DisplayName != "Geo" {
		t.Errorf("valid parse failed: %+v ok=%v", schema, ok)
	}
}
