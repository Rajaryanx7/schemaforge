package schemaforge

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

const previewTTL = 5 * time.Minute

type pendingPreview struct {
	Action  Action
	Expires time.Time
}

type previewStore struct {
	mu       sync.Mutex
	previews map[string]pendingPreview
}

func (c *Client) issuePreview(action Action) (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}

	token := hex.EncodeToString(tokenBytes)

	c.store.mu.Lock()
	defer c.store.mu.Unlock()

	now := time.Now()
	for key, preview := range c.store.previews {
		if !now.Before(preview.Expires) {
			delete(c.store.previews, key)
		}
	}

	c.store.previews[token] = pendingPreview{
		Action:  action,
		Expires: now.Add(previewTTL),
	}

	return token, nil
}

func (c *Client) consumePreview(token string, action Action) error {
	if token == "" {
		return errors.New("preview token is required")
	}

	c.store.mu.Lock()
	defer c.store.mu.Unlock()

	preview, exists := c.store.previews[token]
	if !exists {
		return errors.New("preview token is invalid, expired, or already used")
	}

	// Consume the token even if the action doesn't match.
	delete(c.store.previews, token)

	if !time.Now().Before(preview.Expires) {
		return errors.New("preview token has expired")
	}

	expected, err := json.Marshal(preview.Action)
	if err != nil {
		return err
	}

	actual, err := json.Marshal(action)
	if err != nil {
		return err
	}

	if string(expected) != string(actual) {
		return errors.New("action differs from the previewed action")
	}

	return nil
}
