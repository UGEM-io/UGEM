// Copyright (c) 2026 UGEM Community
// Licensed under the GNU Affero General Public License v3.0.
// See LICENSE_AGPL.md in the project root for full license information.
package security

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/ugem-io/ugem/logging"
)

type User struct {
	ID        string
	Username  string
	Password  string
	Roles     []string
	CreatedAt time.Time
	LastLogin time.Time
	Active    bool
}

type Role struct {
	Name        string
	Permissions []string
}

type Permission string

const (
	PermissionRead    Permission = "read"
	PermissionWrite   Permission = "write"
	PermissionDelete  Permission = "delete"
	PermissionAdmin   Permission = "admin"
	PermissionExecute Permission = "execute"
)

type Authenticator struct {
	users    map[string]*User
	roles    map[string]*Role
	mu       sync.RWMutex
	sessions map[string]*Session
}

type Session struct {
	ID        string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
	Token     string
}

func NewAuthenticator() *Authenticator {
	return &Authenticator{
		users:    make(map[string]*User),
		roles:    make(map[string]*Role),
		sessions: make(map[string]*Session),
	}
}

func (a *Authenticator) AddUser(username, password string, roles []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if _, exists := a.users[username]; exists {
		return fmt.Errorf("user %s already exists", username)
	}

	hashedPassword := hashPassword(password)

	user := &User{
		ID:        fmt.Sprintf("user-%d", time.Now().UnixNano()),
		Username:  username,
		Password:  hashedPassword,
		Roles:     roles,
		CreatedAt: time.Now(),
		Active:    true,
	}

	a.users[username] = user
	logging.Info("user added", logging.Field{"username": username, "roles": roles})

	return nil
}

func (a *Authenticator) RemoveUser(username string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	user, exists := a.users[username]
	if !exists {
		return fmt.Errorf("user %s not found", username)
	}

	user.Active = false
	logging.Info("user removed", logging.Field{"username": username})

	return nil
}

func (a *Authenticator) Authenticate(username, password string) (*Session, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	user, exists := a.users[username]
	if !exists || !user.Active {
		return nil, fmt.Errorf("invalid credentials")
	}

	hashedPassword := hashPassword(password)
	if subtle.ConstantTimeCompare([]byte(user.Password), []byte(hashedPassword)) != 1 {
		return nil, fmt.Errorf("invalid credentials")
	}

	token := generateToken(username)
	session := &Session{
		ID:        fmt.Sprintf("session-%d", time.Now().UnixNano()),
		UserID:    user.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
		Token:     token,
	}

	a.sessions[token] = session

	user.LastLogin = time.Now()
	logging.Info("user authenticated", logging.Field{"username": username})

	return session, nil
}

func (a *Authenticator) ValidateSession(token string) (*Session, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	session, exists := a.sessions[token]
	if !exists {
		return nil, fmt.Errorf("invalid session")
	}

	if time.Now().After(session.ExpiresAt) {
		delete(a.sessions, token)
		return nil, fmt.Errorf("session expired")
	}

	return session, nil
}

func (a *Authenticator) RevokeSession(token string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.sessions, token)
	logging.Info("session revoked", logging.Field{"token": token[:10]})

	return nil
}

func (a *Authenticator) AddRole(name string, permissions []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if _, exists := a.roles[name]; exists {
		return fmt.Errorf("role %s already exists", name)
	}

	role := &Role{
		Name:        name,
		Permissions: permissions,
	}

	a.roles[name] = role
	logging.Info("role added", logging.Field{"role": name, "permissions": permissions})

	return nil
}

func (a *Authenticator) HasPermission(username string, permission Permission) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	user, exists := a.users[username]
	if !exists || !user.Active {
		return false
	}

	for _, roleName := range user.Roles {
		role, exists := a.roles[roleName]
		if !exists {
			continue
		}

		for _, perm := range role.Permissions {
			if perm == string(permission) || perm == string(PermissionAdmin) {
				return true
			}
		}
	}

	return false
}

func (a *Authenticator) CheckPermission(ctx context.Context, permission Permission) error {
	token, ok := ctx.Value("token").(string)
	if !ok {
		return fmt.Errorf("no token in context")
	}

	session, err := a.ValidateSession(token)
	if err != nil {
		return err
	}

	a.mu.RLock()
	user, exists := a.users[session.UserID]
	a.mu.RUnlock()

	if !exists {
		return fmt.Errorf("user not found")
	}

	if !a.HasPermission(user.Username, permission) {
		return fmt.Errorf("permission denied")
	}

	return nil
}

type Authorizer struct {
	policies map[string][]PolicyRule
	auth     *Authenticator
	mu       sync.RWMutex
}

type PolicyRule struct {
	Effect     string
	Actions    []string
	Resources  []string
	Conditions map[string]string
}

func NewAuthorizer(auth *Authenticator) *Authorizer {
	return &Authorizer{
		policies: make(map[string][]PolicyRule),
		auth:     auth,
	}
}

func (az *Authorizer) AddPolicy(name string, rules []PolicyRule) {
	az.mu.Lock()
	defer az.mu.Unlock()
	az.policies[name] = rules
	logging.Info("policy added", logging.Field{"name": name, "rules": len(rules)})
}

func (az *Authorizer) Evaluate(ctx context.Context, action, resource string) (bool, error) {
	if err := az.auth.CheckPermission(ctx, PermissionRead); err != nil {
		return false, err
	}

	az.mu.RLock()
	defer az.mu.RUnlock()

	for _, rules := range az.policies {
		for _, rule := range rules {
			if matchesAction(rule.Actions, action) && matchesResource(rule.Resources, resource) {
				return rule.Effect == "allow", nil
			}
		}
	}

	return false, nil
}

func matchesAction(actions []string, action string) bool {
	for _, a := range actions {
		if a == "*" || a == action {
			return true
		}
	}
	return false
}

func matchesResource(resources []string, resource string) bool {
	for _, r := range resources {
		if r == "*" || r == resource {
			return true
		}
	}
	return false
}

func hashPassword(password string) string {
	hash := sha256.Sum256([]byte(password))
	return hex.EncodeToString(hash[:])
}

func generateToken(username string) string {
	data := fmt.Sprintf("%s-%d-%s", username, time.Now().UnixNano(), "secret")
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

func WithAuth(auth *Authenticator) func(context.Context) context.Context {
	return func(ctx context.Context) context.Context {
		return context.WithValue(ctx, "auth", auth)
	}
}

func GetUserFromContext(ctx context.Context) (*User, error) {
	token, ok := ctx.Value("token").(string)
	if !ok {
		return nil, fmt.Errorf("no token in context")
	}

	auth, ok := ctx.Value("auth").(*Authenticator)
	if !ok {
		return nil, fmt.Errorf("no auth in context")
	}

	session, err := auth.ValidateSession(token)
	if err != nil {
		return nil, err
	}

	auth.mu.RLock()
	user := auth.users[session.UserID]
	auth.mu.RUnlock()

	if user == nil {
		return nil, fmt.Errorf("user not found")
	}

	return user, nil
}
