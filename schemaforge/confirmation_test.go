package schemaforge

import (
	"strings"
	"testing"
	"time"
)

func TestPreviewTokenLifecycle(t *testing.T) {
	c := &Client{
		store: previewStore{
			previews: make(map[string]pendingPreview),
		},
	}

	action := Action{
		Type:  ActionUpdate,
		Table: "tasks",
		ID:    3,
		Values: map[string]any{
			"completed": true,
		},
	}

	token, err := c.issuePreview(action)
	if err != nil {
		t.Fatalf("issuePreview: %v", err)
	}

	t.Run("valid token", func(t *testing.T) {
		if err := c.consumePreview(token, action); err != nil {
			t.Fatalf("expected valid token: %v", err)
		}
	})

	t.Run("reused token", func(t *testing.T) {
		err := c.consumePreview(token, action)
		if err == nil || !strings.Contains(err.Error(), "invalid") {
			t.Fatalf("expected consumed token to be rejected, got %v", err)
		}
	})
}

func TestPreviewTokenRejectsModifiedAction(t *testing.T) {
	c := &Client{
		store: previewStore{
			previews: make(map[string]pendingPreview),
		},
	}

	original := Action{
		Type:  ActionDelete,
		Table: "tasks",
		ID:    7,
	}

	token, err := c.issuePreview(original)
	if err != nil {
		t.Fatal(err)
	}

	modified := original
	modified.ID = 8

	if err := c.consumePreview(token, modified); err == nil {
		t.Fatal("expected modified action to be rejected")
	}
}

func TestExpiredPreviewToken(t *testing.T) {
	c := &Client{
		store: previewStore{
			previews: make(map[string]pendingPreview),
		},
	}

	action := Action{
		Type:  ActionDelete,
		Table: "tasks",
		ID:    7,
	}

	token, err := c.issuePreview(action)
	if err != nil {
		t.Fatal(err)
	}

	c.store.mu.Lock()
	preview := c.store.previews[token]
	preview.Expires = time.Now().Add(-time.Second)
	c.store.previews[token] = preview
	c.store.mu.Unlock()

	if err := c.consumePreview(token, action); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}
