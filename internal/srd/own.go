package srd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type ownRecord struct {
	UserID           string `json:"userId"`
	SessionID        string `json:"sessionId"`
	CFToken          string `json:"cfToken"`
	RefreshToken     string `json:"refreshToken,omitempty"`
	CFExpiresAt      int64  `json:"cfExpiresAt,omitempty"`
	RefreshExpiresAt int64  `json:"refreshExpiresAt,omitempty"`
}

// LoadOwn reads the session saved by `codefree2api login`. A missing file
// is not an error.
func LoadOwn(kind Kind) (*Session, error) {
	path, err := ownPath(kind)
	if err != nil {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, nil
	}
	var rec ownRecord
	if json.Unmarshal(b, &rec) != nil {
		return nil, nil
	}
	sess := &Session{
		UserID:           rec.UserID,
		SessionID:        rec.SessionID,
		CFToken:          rec.CFToken,
		RefreshToken:     rec.RefreshToken,
		CFExpiresAt:      rec.CFExpiresAt,
		RefreshExpiresAt: rec.RefreshExpiresAt,
	}
	if sess.UserID == "" || (sess.CFToken == "" && sess.RefreshToken == "") {
		return nil, nil
	}
	return sess, nil
}

// SaveOwn writes the session for later commands. The file is not a log.
func SaveOwn(kind Kind, sess *Session) error {
	if sess == nil || sess.UserID == "" || sess.CFToken == "" {
		return ErrLogin
	}
	path, err := ownPath(kind)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(ownRecord{
		UserID:           sess.UserID,
		SessionID:        sess.SessionID,
		CFToken:          sess.CFToken,
		RefreshToken:     sess.RefreshToken,
		CFExpiresAt:      sess.CFExpiresAt,
		RefreshExpiresAt: sess.RefreshExpiresAt,
	})
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DeleteOwn removes the saved session after the server rejected it.
func DeleteOwn(kind Kind) error {
	path, err := ownPath(kind)
	if err != nil {
		return nil
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func ownPath(kind Kind) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	name := string(kind)
	if name == "" {
		return "", ErrLogin
	}
	return filepath.Join(dir, "codefree2api", name+".json"), nil
}
