package schemaforge

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestConvertActionValue(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		dataType string
		want     any
		wantErr  bool
	}{
		{"nil value", nil, "text", nil, false},

		{"text", "hello", "text", "hello", false},
		{"varchar", "hello", "character varying", "hello", false},
		{"character", "x", "character", "x", false},
		{"text wrong type", 123, "text", nil, true},
		{"text with NUL", "hello\x00world", "text", nil, true},

		{"smallint", json.Number("123"), "smallint", int64(123), false},
		{"integer from int", 123, "integer", int64(123), false},
		{"bigint from int64", int64(123), "bigint", int64(123), false},
		{"integer from json number", json.Number("-42"), "integer", int64(-42), false},
		{"integer rejects float", float64(1), "integer", nil, true},
		{"integer rejects string", "123", "integer", nil, true},
		{"integer rejects decimal", json.Number("1.5"), "integer", nil, true},
		{"smallint overflow", json.Number("40000"), "smallint", nil, true},
		{"integer overflow", json.Number("2147483648"), "integer", nil, true},
		{"invalid integer", json.Number("abc"), "bigint", nil, true},

		{"boolean true", true, "boolean", true, false},
		{"boolean false", false, "boolean", false, false},
		{"boolean rejects string", "true", "boolean", nil, true},

		{"unsupported type", "hello", "uuid", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertActionValue(tt.value, tt.dataType)

			if (err != nil) != tt.wantErr {
				t.Fatalf("convertActionValue() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err == nil && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("convertActionValue() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
