package schemaforge

import "testing"

func TestActionValidate(t *testing.T) {
	tests := []struct {
		name    string
		action  Action
		wantErr bool
	}{
		{
			name: "valid insert",
			action: Action{
				Type:  ActionInsert,
				Table: "tasks",
				Values: map[string]any{
					"title": "Finish project",
				},
			},
		},
		{
			name: "valid update",
			action: Action{
				Type:  ActionUpdate,
				Table: "tasks",
				ID:    3,
				Values: map[string]any{
					"completed": true,
				},
			},
		},
		{
			name: "valid delete",
			action: Action{
				Type:  ActionDelete,
				Table: "tasks",
				ID:    3,
			},
		},
		{
			name: "missing table",
			action: Action{
				Type: ActionDelete,
				ID:   3,
			},
			wantErr: true,
		},
		{
			name: "insert without values",
			action: Action{
				Type:  ActionInsert,
				Table: "tasks",
			},
			wantErr: true,
		},
		{
			name: "update without ID",
			action: Action{
				Type:  ActionUpdate,
				Table: "tasks",
				Values: map[string]any{
					"completed": true,
				},
			},
			wantErr: true,
		},
		{
			name: "delete without ID",
			action: Action{
				Type:  ActionDelete,
				Table: "tasks",
				ID:    0,
			},
			wantErr: true,
		},
		{
			name: "unsupported action",
			action: Action{
				Type:  "drop",
				Table: "tasks",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.action.Validate()

			if tt.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}

			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
