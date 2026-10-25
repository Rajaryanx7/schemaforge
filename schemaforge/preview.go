package schemaforge

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Preview converts a natural-language instruction into a validated database
// action and returns the action with a short-lived, single-use confirmation
// token. It does not modify the database.
//
// The instruction must not be empty and cannot exceed 2000 characters.
// The returned token is required to execute the exact previewed action.
func (c *Client) Preview(
	ctx context.Context,
	instruction string,
) (*Action, string, error) {
	if c == nil || c.db == nil {
		return nil, "", errors.New("SchemaForge client is not initialized")
	}

	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return nil, "", errors.New("instruction cannot be empty")
	}

	if len(instruction) > 2000 {
		return nil, "", errors.New("instruction cannot exceed 2000 characters")
	}

	ctx, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()

	action, err := c.parseInstruction(ctx, instruction)
	if err != nil {
		return nil, "", err
	}

	if err := action.Validate(); err != nil {
		return nil, "", err
	}

	if err := c.validateAction(ctx, action); err != nil {
		return nil, "", err
	}

	token, err := c.issuePreview(*action)
	if err != nil {
		return nil, "", err
	}

	return action, token, nil
}
