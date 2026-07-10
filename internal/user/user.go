// Package user managers all users in libredesk - agents and contacts.
package user

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"log"

	"github.com/abhinavxd/libredesk/internal/dbutil"
	"github.com/abhinavxd/libredesk/internal/envelope"
	"github.com/abhinavxd/libredesk/internal/jsonutil"
	mediamanager "github.com/abhinavxd/libredesk/internal/media"
	rmodels "github.com/abhinavxd/libredesk/internal/role/models"
	"github.com/abhinavxd/libredesk/internal/stringutil"
	"github.com/abhinavxd/libredesk/internal/user/models"
	"github.com/jmoiron/sqlx"
	"github.com/knadh/go-i18n"
	"github.com/lib/pq"
	"github.com/volatiletech/null/v9"
	"github.com/zerodha/logf"
	"golang.org/x/crypto/bcrypt"
)

var (
	//go:embed queries.sql
	efs               embed.FS
	dummyPasswordHash = mustDummyPasswordHash()

	minPassword     = 10
	maxPassword     = 72
	maxListPageSize = 500

	// ErrPasswordTooLong is returned when the password passed to
	// GenerateFromPassword is too long (i.e. > 72 bytes).
	ErrPasswordTooLong = errors.New("password length exceeds 72 bytes")

	PasswordHint = fmt.Sprintf("Password must be %d-%d characters long should contain at least one uppercase letter, one lowercase letter, one number, and one special character.", minPassword, maxPassword)
)

const (
	maxUserCustomAttributeCount      = 100
	maxUserCustomAttributeKeyBytes   = 128
	maxUserCustomAttributesJSONBytes = 64 * 1024
)

const (
	lastActiveFlushDebounce = 30 * time.Second
	agentCacheTTL           = 10 * time.Minute
)

// Manager handles user-related operations.
type Manager struct {
	lo           *logf.Logger
	i18n         *i18n.I18n
	q            queries
	db           *sqlx.DB
	agentCache   map[int]cachedAgent
	agentCacheMu sync.RWMutex
	// Cache versions prevent an in-flight DB read from repopulating stale
	// permissions after an invalidation has completed.
	agentCacheEpoch      uint64
	agentCacheGeneration map[int]uint64

	lastActiveFlushAt   map[int]time.Time
	lastActiveFlushAtMu sync.Mutex
}

type cachedAgent struct {
	user      models.User
	expiresAt time.Time
}

// Opts contains options for initializing the Manager.
type Opts struct {
	DB *sqlx.DB
	Lo *logf.Logger
}

// queries contains prepared SQL queries.
type queries struct {
	GetUser                            *sqlx.Stmt `query:"get-user"`
	GetNotes                           *sqlx.Stmt `query:"get-notes"`
	GetNote                            *sqlx.Stmt `query:"get-note"`
	GetUserIDsByRole                   *sqlx.Stmt `query:"get-user-ids-by-role"`
	GetUserByExternalID                *sqlx.Stmt `query:"get-user-by-external-id"`
	GetUsersCompact                    string     `query:"get-users-compact"`
	UpdateContact                      *sqlx.Stmt `query:"update-contact"`
	UpdateContactBasicInfo             *sqlx.Stmt `query:"update-contact-basic-info"`
	UpdateAgent                        *sqlx.Stmt `query:"update-agent"`
	UpdateCustomAttributes             *sqlx.Stmt `query:"update-custom-attributes"`
	UpsertCustomAttributes             *sqlx.Stmt `query:"upsert-custom-attributes"`
	UpdateAvatar                       *sqlx.Stmt `query:"update-avatar"`
	UpdateAvailability                 *sqlx.Stmt `query:"update-availability"`
	UpdateLastActiveAt                 *sqlx.Stmt `query:"update-last-active-at"`
	UpdateInactiveOffline              *sqlx.Stmt `query:"update-inactive-offline"`
	GetAvailabilityStatus              *sqlx.Stmt `query:"get-availability-status"`
	UpdateLastLoginAt                  *sqlx.Stmt `query:"update-last-login-at"`
	SoftDeleteAgent                    *sqlx.Stmt `query:"soft-delete-agent"`
	SetUserPassword                    *sqlx.Stmt `query:"set-user-password"`
	SetResetPasswordToken              *sqlx.Stmt `query:"set-reset-password-token"`
	GetResetPasswordUserID             *sqlx.Stmt `query:"get-reset-password-user-id"`
	SetPassword                        *sqlx.Stmt `query:"set-password"`
	DeleteExpiredCustomerRegistrations *sqlx.Stmt `query:"delete-expired-customer-registrations"`
	IsCustomerPortalRegisteredByEmail  *sqlx.Stmt `query:"is-customer-portal-registered-by-email"`
	UpsertCustomerPortalRegistration   *sqlx.Stmt `query:"upsert-customer-portal-registration"`
	GetCustomerRegistrationForUpdate   *sqlx.Stmt `query:"get-customer-registration-for-update"`
	GetPortalUserForUpdate             *sqlx.Stmt `query:"get-portal-user-for-update"`
	ActivateExistingPortalUser         *sqlx.Stmt `query:"activate-existing-portal-user"`
	InsertPortalUser                   *sqlx.Stmt `query:"insert-portal-user"`
	DeleteCustomerRegistration         *sqlx.Stmt `query:"delete-customer-registration"`
	DeleteCustomerRegistrationByEmail  *sqlx.Stmt `query:"delete-customer-registration-by-email"`
	DeleteNote                         *sqlx.Stmt `query:"delete-note"`
	InsertAgent                        *sqlx.Stmt `query:"insert-agent"`
	InsertContactWithExtID             *sqlx.Stmt `query:"insert-contact-with-external-id"`
	InsertContactNoExtID               *sqlx.Stmt `query:"insert-contact-without-external-id"`
	GetContactByEmail                  *sqlx.Stmt `query:"get-contact-by-email"`
	GetRegisteredPortalContactByEmail  *sqlx.Stmt `query:"get-registered-portal-contact-by-email"`
	GetContactByEmailWithoutExtID      *sqlx.Stmt `query:"get-contact-by-email-without-ext-id"`
	IsEmailBlocked                     *sqlx.Stmt `query:"is-email-blocked"`
	SetExternalUserID                  *sqlx.Stmt `query:"set-external-user-id"`
	InsertNote                         *sqlx.Stmt `query:"insert-note"`
	InsertVisitor                      *sqlx.Stmt `query:"insert-visitor"`
	GetVisitorByEmail                  *sqlx.Stmt `query:"get-visitor-by-email"`
	UpgradeVisitorToContact            *sqlx.Stmt `query:"upgrade-visitor-to-contact"`
	ToggleEnable                       *sqlx.Stmt `query:"toggle-enable"`

	// API key queries
	GetUserByAPIKey      *sqlx.Stmt `query:"get-user-by-api-key"`
	SetAPIKey            *sqlx.Stmt `query:"set-api-key"`
	RevokeAPIKey         *sqlx.Stmt `query:"revoke-api-key"`
	UpdateAPIKeyLastUsed *sqlx.Stmt `query:"update-api-key-last-used"`

	TransferVisitorConversations       *sqlx.Stmt `query:"transfer-visitor-conversations"`
	TransferVisitorMessages            *sqlx.Stmt `query:"transfer-visitor-messages"`
	DeleteDuplicateVisitorParticipants *sqlx.Stmt `query:"delete-duplicate-visitor-participants"`
	TransferVisitorParticipants        *sqlx.Stmt `query:"transfer-visitor-participants"`
	TransferVisitorNotes               *sqlx.Stmt `query:"transfer-visitor-notes"`
	TransferVisitorMedia               *sqlx.Stmt `query:"transfer-visitor-media"`
	DeleteMergedVisitor                *sqlx.Stmt `query:"delete-merged-visitor"`
	LockUsersForMerge                  *sqlx.Stmt `query:"lock-users-for-merge"`
	GetMergeMediaUsage                 *sqlx.Stmt `query:"get-merge-media-usage"`
}

// New creates and returns a new instance of the Manager.
func New(i18n *i18n.I18n, opts Opts) (*Manager, error) {
	var q queries
	if err := dbutil.ScanSQLFile("queries.sql", &q, opts.DB, efs); err != nil {
		return nil, fmt.Errorf("error scanning SQL file: %w", err)
	}
	return &Manager{
		q:                    q,
		lo:                   opts.Lo,
		i18n:                 i18n,
		db:                   opts.DB,
		agentCache:           make(map[int]cachedAgent),
		agentCacheGeneration: make(map[int]uint64),
		lastActiveFlushAt:    make(map[int]time.Time),
	}, nil
}

// VerifyPassword authenticates an user by email and password, returning the user if successful.
func (u *Manager) VerifyPassword(email string, password []byte) (models.User, error) {
	var user models.User
	if err := u.q.GetUser.Get(&user, 0, email, pq.Array([]string{models.UserTypeAgent})); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, password)
			return user, envelope.NewError(envelope.InputError, u.i18n.T("user.invalidEmailPassword"), nil)
		}
		u.lo.Error("error fetching user from db", "error", err)
		return user, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	if err := u.verifyPassword(password, user.Password.String); err != nil {
		return user, envelope.NewError(envelope.InputError, u.i18n.T("user.invalidEmailPassword"), nil)
	}
	return user, nil
}

// GetAllUsers returns a list of all users.
func (u *Manager) GetAllUsers(page, pageSize int, userTypes []string, order, orderBy string, filtersJSON, location string) ([]models.UserCompact, error) {
	query, qArgs, err := u.makeUserListQuery(page, pageSize, userTypes, order, orderBy, filtersJSON, location)
	if err != nil {
		u.lo.Error("error creating user list query", "error", err)
		return nil, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	// Start a read-only txn.
	tx, err := u.db.BeginTxx(context.Background(), &sql.TxOptions{
		ReadOnly: true,
	})
	if err != nil {
		u.lo.Error("error starting read-only transaction", "error", err)
		return nil, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	defer tx.Rollback()

	// Execute query
	var users = make([]models.UserCompact, 0)
	if err := tx.Select(&users, query, qArgs...); err != nil {
		u.lo.Error("error fetching users", "error", err)
		return nil, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	return users, nil
}

// Get retrieves an user by ID or email or type. At least one of ID or email must be provided.
func (u *Manager) Get(id int, email string, userType []string) (models.User, error) {
	if id == 0 && email == "" {
		return models.User{}, envelope.NewError(envelope.InputError, u.i18n.T("validation.invalidUser"), nil)
	}

	var user models.User
	if err := u.q.GetUser.Get(&user, id, email, pq.Array(userType)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return user, envelope.NewError(envelope.NotFoundError, u.i18n.T("validation.notFoundUser"), nil)
		}
		u.lo.Error("error fetching user from db", "error", err)
		return user, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return user, nil
}

// GetContactOrVisitor retrieves a user by ID or email that is either a contact or visitor.
func (u *Manager) GetContactOrVisitor(id int, email string) (models.User, error) {
	return u.Get(id, email, []string{models.UserTypeContact, models.UserTypeVisitor})
}

func (u *Manager) GetSystemUser() (models.User, error) {
	return u.Get(0, models.SystemUserEmail, []string{models.UserTypeAgent})
}

// GetByExternalID retrieves a user by external user ID.
func (u *Manager) GetByExternalID(externalUserID string) (models.User, error) {
	var user models.User
	if err := u.q.GetUserByExternalID.Get(&user, externalUserID); err != nil {
		if err == sql.ErrNoRows {
			return user, envelope.NewError(envelope.NotFoundError, u.i18n.T("validation.notFoundUser"), nil)
		}
		u.lo.Error("error fetching user by external ID", "external_user_id", externalUserID, "error", err)
		return user, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return user, nil
}

// GetContactByEmail retrieves a contact by email address regardless of external_user_id.
func (u *Manager) GetContactByEmail(email string) (models.User, error) {
	var user models.User
	if err := u.q.GetContactByEmail.Get(&user, email); err != nil {
		if err == sql.ErrNoRows {
			return user, envelope.NewError(envelope.NotFoundError, u.i18n.T("validation.notFoundUser"), nil)
		}
		u.lo.Error("error fetching contact by email", "email", email, "error", err)
		return user, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return user, nil
}

// GetRegisteredPortalContactByEmail returns the single canonical portal
// identity for an email, even when external integrations created duplicates.
func (u *Manager) GetRegisteredPortalContactByEmail(email string) (models.User, error) {
	var portalUser models.User
	if err := u.q.GetRegisteredPortalContactByEmail.Get(&portalUser, strings.TrimSpace(strings.ToLower(email))); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return portalUser, envelope.NewError(envelope.NotFoundError, u.i18n.T("validation.notFoundUser"), nil)
		}
		u.lo.Error("error fetching registered portal contact by email", "error", err)
		return portalUser, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return portalUser, nil
}

// GetContactByEmailWithoutExtID retrieves a contact by email that has no external_user_id set.
func (u *Manager) GetContactByEmailWithoutExtID(email string) (models.User, error) {
	var user models.User
	if err := u.q.GetContactByEmailWithoutExtID.Get(&user, email); err != nil {
		if err == sql.ErrNoRows {
			return user, envelope.NewError(envelope.NotFoundError, u.i18n.T("validation.notFoundUser"), nil)
		}
		u.lo.Error("error fetching contact by email without ext_id", "email", email, "error", err)
		return user, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return user, nil
}

// IsEmailBlocked checks if any contact or visitor with the given email is blocked.
func (u *Manager) IsEmailBlocked(email string) (bool, error) {
	var blocked bool
	if err := u.q.IsEmailBlocked.Get(&blocked, email); err != nil {
		u.lo.Error("error checking if email is blocked", "email", email, "error", err)
		return false, fmt.Errorf("checking if email is blocked: %w", err)
	}
	return blocked, nil
}

// GetVisitorByEmail retrieves a visitor by email address.
func (u *Manager) GetVisitorByEmail(email string) (models.User, error) {
	var user models.User
	if err := u.q.GetVisitorByEmail.Get(&user, email); err != nil {
		if err == sql.ErrNoRows {
			return user, envelope.NewError(envelope.NotFoundError, u.i18n.T("validation.notFoundUser"), nil)
		}
		u.lo.Error("error fetching visitor by email", "email", email, "error", err)
		return user, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return user, nil
}

// UpgradeVisitorToContact changes a visitor's type to contact.
func (u *Manager) UpgradeVisitorToContact(visitorID int) error {
	if _, err := u.q.UpgradeVisitorToContact.Exec(visitorID); err != nil {
		u.lo.Error("error upgrading visitor to contact", "visitor_id", visitorID, "error", err)
		return fmt.Errorf("upgrading visitor to contact: %w", err)
	}
	return nil
}

// SetExternalUserID sets the external_user_id on an existing contact.
func (u *Manager) SetExternalUserID(id int, externalUserID string) error {
	if _, err := u.q.SetExternalUserID.Exec(id, externalUserID); err != nil {
		u.lo.Error("error setting external user ID", "id", id, "external_user_id", externalUserID, "error", err)
		return fmt.Errorf("setting external user ID: %w", err)
	}
	return nil
}

// UpdateAvatar updates the user avatar.
func (u *Manager) UpdateAvatar(id int, path string) error {
	if _, err := u.q.UpdateAvatar.Exec(id, null.NewString(path, path != "")); err != nil {
		u.lo.Error("error updating user avatar", "error", err)
		return envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return nil
}

// UpdateLastLoginAt updates the last login timestamp of an user.
func (u *Manager) UpdateLastLoginAt(id int) error {
	if _, err := u.q.UpdateLastLoginAt.Exec(id); err != nil {
		u.lo.Error("error updating user last login at", "error", err)
		return envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return nil
}

// SetResetPasswordToken sets a reset password token for an user and returns the token.
func (u *Manager) SetResetPasswordToken(id int) (string, error) {
	token, err := stringutil.RandomAlphanumeric(48)
	if err != nil {
		u.lo.Error("error generating reset password token", "error", err)
		return "", envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	if _, err := u.q.SetResetPasswordToken.Exec(id, securityTokenHash(token)); err != nil {
		u.lo.Error("error setting reset password token", "error", err)
		return "", envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return token, nil
}

// ResetPassword sets a password for a reset token belonging to the expected user type.
func (u *Manager) ResetPassword(token, password, userType string) (int, error) {
	if userType != models.UserTypeAgent && userType != models.UserTypeContact {
		return 0, envelope.NewError(envelope.InputError, u.i18n.T("user.resetPasswordTokenExpired"), nil)
	}
	if !IsStrongPassword(password) {
		return 0, envelope.NewError(envelope.InputError, "Password is not strong enough, "+PasswordHint, nil)
	}
	tokenHash := securityTokenHash(strings.TrimSpace(token))
	if tokenHash == "" {
		return 0, envelope.NewError(envelope.InputError, u.i18n.T("user.resetPasswordTokenExpired"), nil)
	}
	var tokenUserID int
	if err := u.q.GetResetPasswordUserID.Get(&tokenUserID, tokenHash, userType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, envelope.NewError(envelope.InputError, u.i18n.T("user.resetPasswordTokenExpired"), nil)
		}
		u.lo.Error("error validating reset password token", "error", err)
		return 0, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		u.lo.Error("error generating bcrypt password", "error", err)
		return 0, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	var id int
	if err := u.q.SetPassword.Get(&id, passwordHash, tokenHash, userType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, envelope.NewError(envelope.InputError, u.i18n.T("user.resetPasswordTokenExpired"), nil)
		}
		u.lo.Error("error setting new password", "error", err)
		return 0, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return id, nil
}

func securityTokenHash(token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// UpdateAvailability updates the availability status of an user.
func (u *Manager) UpdateAvailability(id int, status string) error {
	if _, err := u.q.UpdateAvailability.Exec(id, status); err != nil {
		u.lo.Error("error updating user availability", "error", err)
		return envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return nil
}

// UpdateLastActive updates last_active_at and returns true if the user flipped from offline to online.
func (u *Manager) UpdateLastActive(id int) (wasOffline bool, err error) {
	agent, cachedOK := u.GetAgentFromCache(id)
	alreadyOnline := cachedOK && agent.AvailabilityStatus == models.Online

	// Already online and within the debounce window - nothing to do.
	if alreadyOnline && !u.reserveFlush(id) {
		return false, nil
	}

	if err := u.q.UpdateLastActiveAt.Get(&wasOffline, id); err != nil {
		u.lo.Error("error updating user last active at", "error", err)
		return false, fmt.Errorf("updating user last active at: %w", err)
	}

	if wasOffline {
		u.InvalidateAgentCache(id)
	}
	return wasOffline, nil
}

// IsOffline returns true if the user's availability status is offline.
func (u *Manager) IsOffline(id int) bool {
	var status string
	if err := u.q.GetAvailabilityStatus.Get(&status, id); err != nil {
		return true
	}
	return status == "offline"
}

// SaveCustomAttributes sets or merges custom attributes for a user.
// If replace is true, existing attributes are overwritten. Otherwise, attributes are merged.
func (u *Manager) SaveCustomAttributes(id int, customAttributes map[string]any, replace bool) error {
	jsonb, err := encodeUserCustomAttributes(customAttributes)
	if err != nil {
		u.lo.Warn("rejecting invalid custom attributes", "error", err)
		return envelope.NewError(envelope.InputError, u.i18n.T("globals.messages.badRequest"), nil)
	}
	var execErr error
	if replace {
		_, execErr = u.q.UpdateCustomAttributes.Exec(id, jsonb)
	} else {
		_, execErr = u.q.UpsertCustomAttributes.Exec(id, jsonb)
	}
	if execErr != nil {
		u.lo.Error("error saving custom attributes", "error", execErr)
		return envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return nil
}

// encodeUserCustomAttributes is the final boundary for every agent and Widget
// custom-attribute write. Authentication state is never a custom attribute.
func encodeUserCustomAttributes(customAttributes map[string]any) ([]byte, error) {
	if err := jsonutil.ValidateSafeObjectKeys(customAttributes, jsonutil.DefaultMaxObjectDepth); err != nil {
		return nil, err
	}
	sanitized := make(map[string]any, len(customAttributes))
	for key, value := range customAttributes {
		if key == customerPortalRegisteredKey {
			continue
		}
		if key == "" || len(key) > maxUserCustomAttributeKeyBytes {
			return nil, fmt.Errorf("invalid custom attribute key")
		}
		sanitized[key] = value
	}
	if len(sanitized) > maxUserCustomAttributeCount {
		return nil, fmt.Errorf("too many custom attributes")
	}
	jsonb, err := json.Marshal(sanitized)
	if err != nil {
		return nil, err
	}
	if len(jsonb) > maxUserCustomAttributesJSONBytes {
		return nil, fmt.Errorf("custom attributes exceed size limit")
	}
	return jsonb, nil
}

// ToggleEnabled toggles the enabled status of an user.
func (u *Manager) ToggleEnabled(id int, typ string, enabled bool) error {
	if _, err := u.q.ToggleEnable.Exec(id, typ, enabled); err != nil {
		u.lo.Error("error toggling user enabled status", "error", err)
		return envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return nil
}

// GenerateAPIKey generates a new API key and secret for a user
func (u *Manager) GenerateAPIKey(userID int) (string, string, error) {
	// Generate API key (32 characters)
	apiKey, err := stringutil.RandomAlphanumeric(32)
	if err != nil {
		u.lo.Error("error generating API key", "error", err, "user_id", userID)
		return "", "", envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	// Generate API secret (64 characters)
	apiSecret, err := stringutil.RandomAlphanumeric(64)
	if err != nil {
		u.lo.Error("error generating API secret", "error", err, "user_id", userID)
		return "", "", envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	// Hash the API secret for storage
	secretHash, err := bcrypt.GenerateFromPassword([]byte(apiSecret), bcrypt.DefaultCost)
	if err != nil {
		u.lo.Error("error hashing API secret", "error", err, "user_id", userID)
		return "", "", envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	// Update user with API key.
	if _, err := u.q.SetAPIKey.Exec(userID, apiKey, string(secretHash)); err != nil {
		u.lo.Error("error saving API key", "error", err, "user_id", userID)
		return "", "", envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	return apiKey, apiSecret, nil
}

// ValidateAPIKey validates API key and secret and returns the user
func (u *Manager) ValidateAPIKey(apiKey, apiSecret string) (models.User, error) {
	var user models.User

	// Find user by API key.
	if err := u.q.GetUserByAPIKey.Get(&user, apiKey); err != nil {
		if err == sql.ErrNoRows {
			return user, envelope.NewError(envelope.UnauthorizedError, u.i18n.T("validation.invalidCredential"), nil)
		}
		return user, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	// Verify API secret.
	if err := bcrypt.CompareHashAndPassword([]byte(user.APISecret.String), []byte(apiSecret)); err != nil {
		return user, envelope.NewError(envelope.UnauthorizedError, u.i18n.T("validation.invalidCredential"), nil)
	}

	// Update last used timestamp.
	if _, err := u.q.UpdateAPIKeyLastUsed.Exec(user.ID); err != nil {
		u.lo.Error("failed to update API key last used timestamp", "error", err, "user_id", user.ID)
	}

	return user, nil
}

// RevokeAPIKey deactivates the API key for a user
func (u *Manager) RevokeAPIKey(userID int) error {
	if _, err := u.q.RevokeAPIKey.Exec(userID); err != nil {
		u.lo.Error("error revoking API key", "error", err, "user_id", userID)
		return envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return nil
}

// MergeVisitorToContact transfers conversations from visitor to contact and deletes the visitor.
func (u *Manager) MergeVisitorToContact(visitorID, contactID int) error {
	if visitorID <= 0 || contactID <= 0 || visitorID == contactID {
		return envelope.NewError(envelope.InputError, u.i18n.T("globals.messages.badRequest"), nil)
	}
	tx, err := u.db.Beginx()
	if err != nil {
		return envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	defer tx.Rollback()

	type mergeUser struct {
		ID   int    `db:"id"`
		Type string `db:"type"`
	}
	var locked []mergeUser
	if err := tx.Stmtx(u.q.LockUsersForMerge).Select(&locked, pq.Array([]int{visitorID, contactID})); err != nil || len(locked) != 2 {
		u.lo.Warn("invalid users in visitor merge", "visitor_id", visitorID, "contact_id", contactID, "error", err)
		return envelope.NewError(envelope.PermissionError, u.i18n.T("status.deniedPermission"), nil)
	}
	validVisitor, validContact := false, false
	for _, item := range locked {
		validVisitor = validVisitor || (item.ID == visitorID && item.Type == models.UserTypeVisitor)
		validContact = validContact || (item.ID == contactID && item.Type == models.UserTypeContact)
	}
	if !validVisitor || !validContact {
		return envelope.NewError(envelope.PermissionError, u.i18n.T("status.deniedPermission"), nil)
	}

	var mediaCount, mediaBytes int64
	if err := tx.Stmtx(u.q.GetMergeMediaUsage).QueryRow(pq.Array([]int{visitorID, contactID})).Scan(&mediaCount, &mediaBytes); err != nil {
		u.lo.Error("error checking media quota for visitor merge", "error", err)
		return envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	if !mediamanager.OwnedMediaUsageWithinLimits(mediaCount, mediaBytes) {
		return envelope.NewErrorWithCode(envelope.InputError, 413, "Persistent upload quota exceeded", nil)
	}

	mergeSteps := []*sqlx.Stmt{
		u.q.TransferVisitorConversations,
		u.q.TransferVisitorMessages,
		u.q.DeleteDuplicateVisitorParticipants,
		u.q.TransferVisitorParticipants,
		u.q.TransferVisitorNotes,
		u.q.TransferVisitorMedia,
	}
	for _, step := range mergeSteps {
		if _, err := tx.Stmtx(step).Exec(visitorID, contactID); err != nil {
			u.lo.Error("error merging visitor data to contact", "visitor_id", visitorID, "contact_id", contactID, "error", err)
			return envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
		}
	}
	result, err := tx.Stmtx(u.q.DeleteMergedVisitor).Exec(visitorID)
	if err != nil {
		u.lo.Error("error deleting merged visitor", "visitor_id", visitorID, "error", err)
		return envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return envelope.NewError(envelope.ConflictError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	if err := tx.Commit(); err != nil {
		u.lo.Error("error committing visitor merge", "error", err)
		return envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return nil
}

func (u *Manager) GetUserIDsByRole(roleID int) ([]int, error) {
	var ids []int
	if err := u.q.GetUserIDsByRole.Select(&ids, roleID); err != nil {
		u.lo.Error("error fetching user ids by role", "role_id", roleID, "error", err)
		return nil, err
	}
	return ids, nil
}

// ChangeSystemUserPassword updates the system user's password with a newly prompted one.
func ChangeSystemUserPassword(ctx context.Context, db *sqlx.DB) error {
	// Prompt for password and get hashed password
	hashedPassword, err := promptAndHashPassword(ctx)
	if err != nil {
		return err
	}

	// Update system user's password in the database.
	if err := updateSystemUserPassword(db, hashedPassword); err != nil {
		return fmt.Errorf("error updating system user password: %v", err)
	}
	fmt.Println("password updated successfully. Login with email 'System' and the new password.")
	return nil
}

// CreateSystemUser creates a system user with the provided password or a random one.
func CreateSystemUser(ctx context.Context, password string, db *sqlx.DB) error {
	var err error

	// Set random password if not provided.
	if password == "" {
		password, err = stringutil.RandomAlphanumeric(32)
		if err != nil {
			return fmt.Errorf("failed to generate system used password: %v", err)
		}
	} else {
		log.Print("using provided password for system user")
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash system user password: %v", err)
	}

	_, err = db.Exec(`
		WITH sys_user AS (
			INSERT INTO users (email, type, first_name, last_name, password)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id
		)
		INSERT INTO user_roles (user_id, role_id)
		SELECT sys_user.id, roles.id 
		FROM sys_user, roles 
		WHERE roles.name = $6`,
		models.SystemUserEmail, models.UserTypeAgent, "System", "", hashedPassword, rmodels.RoleAdmin)
	if err != nil {
		return fmt.Errorf("failed to create system user: %v", err)
	}
	log.Print("system user created successfully. Use command 'libredesk --set-system-user-password' to set the password and login with email 'System'.")
	return nil
}

// IsStrongPassword checks if the password meets the required strength for system user.
func IsStrongPassword(password string) bool {
	if len(password) < minPassword || len(password) > maxPassword {
		return false
	}
	hasUppercase := regexp.MustCompile(`[A-Z]`).MatchString(password)
	hasLowercase := regexp.MustCompile(`[a-z]`).MatchString(password)
	hasNumber := regexp.MustCompile(`[0-9]`).MatchString(password)
	// Matches special characters
	hasSpecial := regexp.MustCompile(`[\W_]`).MatchString(password)
	return hasUppercase && hasLowercase && hasNumber && hasSpecial
}

// promptAndHashPassword handles password input and validation, and returns the hashed password.
func promptAndHashPassword(ctx context.Context) ([]byte, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			fmt.Printf("Please set System user password (%s): ", PasswordHint)
			buffer := make([]byte, 256)
			n, err := os.Stdin.Read(buffer)
			if err != nil {
				return nil, fmt.Errorf("error reading input: %v", err)
			}
			password := strings.TrimSpace(string(buffer[:n]))
			if IsStrongPassword(password) {
				// Hash the password using bcrypt.
				hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
				if err != nil {
					return nil, fmt.Errorf("failed to hash password: %v", err)
				}
				return hashedPassword, nil
			}
			fmt.Println("Password does not meet the strength requirements.")
		}
	}
}

// updateSystemUserPassword updates the password of the system user in the database.
func updateSystemUserPassword(db *sqlx.DB, hashedPassword []byte) error {
	_, err := db.Exec(`
		UPDATE users
		SET password = $1,
			session_version = session_version + 1,
			api_key = NULL,
			api_secret = NULL,
			api_key_last_used_at = NULL,
			updated_at = now()
		WHERE email = $2`, hashedPassword, models.SystemUserEmail)
	if err != nil {
		return fmt.Errorf("failed to update system user password: %v", err)
	}
	return nil
}

// makeUserListQuery generates a query to fetch users based on the provided filters.
func (u *Manager) makeUserListQuery(page, pageSize int, userTypes []string, order, orderBy, filtersJSON, location string) (string, []interface{}, error) {
	var qArgs []any
	qArgs = append(qArgs, pq.Array(userTypes))
	return dbutil.BuildPaginatedQuery(u.q.GetUsersCompact, qArgs, dbutil.PaginationOptions{
		Order:    order,
		OrderBy:  orderBy,
		Page:     page,
		PageSize: pageSize,
		Location: location,
	}, filtersJSON, dbutil.AllowedFields{
		"users": {"email", "created_at", "updated_at"},
	}, nil)
}

// verifyPassword compares the provided password with the stored password hash.
func (u *Manager) verifyPassword(pwd []byte, pwdHash string) error {
	if pwdHash == "" {
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, pwd)
		return bcrypt.ErrMismatchedHashAndPassword
	}
	if err := bcrypt.CompareHashAndPassword([]byte(pwdHash), pwd); err != nil {
		if !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			u.lo.Error("error verifying password hash", "error", err)
		}
		return fmt.Errorf("error verifying password: %w", err)
	}
	return nil
}

func mustDummyPasswordHash() []byte {
	hash, err := bcrypt.GenerateFromPassword([]byte("NotARealPassword1!"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return hash
}

// generatePassword generates a random password and returns its bcrypt hash.
func (u *Manager) generatePassword() ([]byte, error) {
	password, _ := stringutil.RandomAlphanumeric(70)
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		u.lo.Error("error generating bcrypt password", "error", err)
		return nil, fmt.Errorf("generating bcrypt password: %w", err)
	}
	return bytes, nil
}

// reserveFlush atomically claims the flush slot, returning false if still inside the debounce window.
func (u *Manager) reserveFlush(id int) bool {
	u.lastActiveFlushAtMu.Lock()
	defer u.lastActiveFlushAtMu.Unlock()
	if last, ok := u.lastActiveFlushAt[id]; ok && time.Since(last) < lastActiveFlushDebounce {
		return false
	}
	// Stamp timestamp.
	u.lastActiveFlushAt[id] = time.Now()
	return true
}
