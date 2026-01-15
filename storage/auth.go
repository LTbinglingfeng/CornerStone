package storage

import (
	"cornerstone/logging"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrAuthNotSetup        = errors.New("auth not setup")
	ErrAuthExists          = errors.New("auth already exists")
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrInvalidAuthUsername = errors.New("invalid auth username")
	ErrInvalidAuthPassword = errors.New("invalid auth password")
)

// AuthInfo stores credential metadata in user_about/password.json.
type AuthInfo struct {
	UserID       string    `json:"user_id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"`
	PasswordSalt string    `json:"password_salt"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AuthManager manages auth persistence.
type AuthManager struct {
	baseDir  string
	authInfo *AuthInfo
	mu       sync.RWMutex
}

// NewAuthManager creates a new auth manager.
func NewAuthManager(baseDir string) *AuthManager {
	am := &AuthManager{
		baseDir: baseDir,
	}
	_ = os.MkdirAll(baseDir, 0755)
	_ = am.Load()
	return am
}

func (am *AuthManager) authPath() string {
	return filepath.Join(am.baseDir, "password.json")
}

// Load reads auth info from disk if present.
func (am *AuthManager) Load() error {
	am.mu.Lock()
	defer am.mu.Unlock()

	data, err := os.ReadFile(am.authPath())
	if err != nil {
		if os.IsNotExist(err) {
			am.authInfo = nil
			return nil
		}
		logging.Errorf("auth load failed: err=%v", err)
		return err
	}

	var info AuthInfo
	if err := json.Unmarshal(data, &info); err != nil {
		logging.Errorf("auth parse failed: err=%v", err)
		return err
	}
	am.authInfo = &info
	return nil
}

// IsSetup returns true when auth is initialized.
func (am *AuthManager) IsSetup() bool {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.authInfo != nil
}

// GetInfo returns a copy of auth info if it exists.
func (am *AuthManager) GetInfo() *AuthInfo {
	am.mu.RLock()
	defer am.mu.RUnlock()
	if am.authInfo == nil {
		return nil
	}
	info := *am.authInfo
	return &info
}

// Setup initializes auth with username and password.
func (am *AuthManager) Setup(username, password string) (*AuthInfo, error) {
	if username == "" {
		return nil, ErrInvalidAuthUsername
	}
	if password == "" {
		return nil, ErrInvalidAuthPassword
	}

	am.mu.Lock()
	defer am.mu.Unlock()

	if am.authInfo != nil {
		return nil, ErrAuthExists
	}

	userID, err := generateUserID()
	if err != nil {
		return nil, err
	}
	salt, err := generateSalt()
	if err != nil {
		return nil, err
	}
	hashed := hashPassword(password, salt)
	now := time.Now()

	info := &AuthInfo{
		UserID:       userID,
		Username:     username,
		PasswordHash: hashed,
		PasswordSalt: salt,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	am.authInfo = info

	if err := am.saveUnsafe(); err != nil {
		am.authInfo = nil
		return nil, err
	}

	copyInfo := *info
	logging.Infof("auth setup completed: username=%s", info.Username)
	return &copyInfo, nil
}

// Verify checks credentials and returns auth info when valid.
func (am *AuthManager) Verify(username, password string) (*AuthInfo, error) {
	if password == "" {
		return nil, ErrInvalidCredentials
	}

	am.mu.RLock()
	info := am.authInfo
	am.mu.RUnlock()

	if info == nil {
		return nil, ErrAuthNotSetup
	}
	if username != "" && info.Username != username {
		return nil, ErrInvalidCredentials
	}

	computed := hashPassword(password, info.PasswordSalt)
	if subtle.ConstantTimeCompare([]byte(computed), []byte(info.PasswordHash)) != 1 {
		return nil, ErrInvalidCredentials
	}

	copyInfo := *info
	return &copyInfo, nil
}

// UpdateUsername syncs the stored auth username.
func (am *AuthManager) UpdateUsername(username string) error {
	if username == "" {
		return ErrInvalidAuthUsername
	}

	am.mu.Lock()
	defer am.mu.Unlock()

	if am.authInfo == nil {
		return ErrAuthNotSetup
	}
	am.authInfo.Username = username
	am.authInfo.UpdatedAt = time.Now()
	return am.saveUnsafe()
}

func (am *AuthManager) saveUnsafe() error {
	if am.authInfo == nil {
		return ErrAuthNotSetup
	}
	data, err := json.MarshalIndent(am.authInfo, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(am.authPath(), data, 0600); err != nil {
		return err
	}
	_ = os.Chmod(am.authPath(), 0600)
	return nil
}

func generateUserID() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(900))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%03d", int(n.Int64())+100), nil
}

func generateSalt() (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	return hex.EncodeToString(salt), nil
}

func hashPassword(password, salt string) string {
	sum := sha256.Sum256([]byte(salt + ":" + password))
	return hex.EncodeToString(sum[:])
}
