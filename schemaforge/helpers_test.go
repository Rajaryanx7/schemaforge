package schemaforge

import "testing"

func TestQualifiedTable(t *testing.T) {
	got := qualifiedTable("students")
	want := `public."students"`

	if got != want {
		t.Errorf("qualifiedTable() = %q, want %q", got, want)
	}
}

func TestQuotedColumns(t *testing.T) {
	tests := []struct {
		name string
		cols []string
		want string
	}{
		{
			name: "multiple columns",
			cols: []string{"id", "first_name", "email"},
			want: `"id", "first_name", "email"`,
		},
		{
			name: "single column",
			cols: []string{"title"},
			want: `"title"`,
		},
		{
			name: "no columns",
			cols: []string{},
			want: "",
		},
		{
			name: "identifier containing quote",
			cols: []string{`user"name`},
			want: `"user""name"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := quotedColumns(tt.cols)
			if got != tt.want {
				t.Errorf("quotedColumns() = %q, want %q", got, tt.want)
			}
		})
	}
}
