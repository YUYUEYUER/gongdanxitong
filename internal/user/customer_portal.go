package user

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/abhinavxd/libredesk/internal/envelope"
	"github.com/abhinavxd/libredesk/internal/stringutil"
	"github.com/abhinavxd/libredesk/internal/user/models"
	"golang.org/x/crypto/bcrypt"
)

const customerPortalRegisteredKey = "portal_registered"

func IsCustomerPortalRegistered(user models.User) bool {
	return user.PortalRegistered
}

func (u *Manager) VerifyContactPassword(email string, password []byte) (models.User, error) {
	var user models.User

	user, err := u.GetRegisteredPortalContactByEmail(email)
	if err != nil {
		if envErr, ok := err.(envelope.Error); ok && envErr.ErrorType == envelope.NotFoundError {
			_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, password)
			return user, envelope.NewError(envelope.InputError, u.i18n.T("user.invalidEmailPassword"), nil)
		}
		return user, err
	}

	if err := u.verifyPassword(password, user.Password.String); err != nil {
		return user, envelope.NewError(envelope.InputError, u.i18n.T("user.invalidEmailPassword"), nil)
	}

	return user, nil
}

type pendingCustomerRegistration struct {
	ID        int    `db:"id"`
	Email     string `db:"email"`
	FirstName string `db:"first_name"`
	LastName  string `db:"last_name"`
}

type portalUserState struct {
	ID               int    `db:"id"`
	Type             string `db:"type"`
	Enabled          bool   `db:"enabled"`
	PortalRegistered bool   `db:"portal_registered"`
}

// BeginCustomerPortalRegistration records a short-lived registration request.
// It deliberately does not mutate users or expose whether the email exists.
func (u *Manager) BeginCustomerPortalRegistration(firstName, lastName, email string) (string, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	firstName = strings.TrimSpace(firstName)
	lastName = strings.TrimSpace(lastName)

	blocked, err := u.IsEmailBlocked(email)
	if err != nil {
		return "", envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	var alreadyRegistered bool
	if err := u.q.IsCustomerPortalRegisteredByEmail.Get(&alreadyRegistered, email); err != nil {
		u.lo.Error("error checking portal registration state", "error", err)
		return "", envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	if blocked || alreadyRegistered {
		return "", nil
	}

	token, err := stringutil.RandomAlphanumeric(48)
	if err != nil {
		u.lo.Error("error generating customer registration token", "error", err)
		return "", envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	if _, err := u.q.DeleteExpiredCustomerRegistrations.Exec(); err != nil {
		u.lo.Warn("error cleaning expired customer registrations", "error", err)
	}
	if _, err := u.q.UpsertCustomerPortalRegistration.Exec(
		email,
		firstName,
		lastName,
		securityTokenHash(token),
	); err != nil {
		u.lo.Error("error storing customer registration", "error", err)
		return "", envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	return token, nil
}

// VerifyCustomerPortalRegistration atomically consumes a verification token
// and only then creates or activates the customer portal account.
func (u *Manager) VerifyCustomerPortalRegistration(token, password string) (models.User, error) {
	tokenHash := securityTokenHash(strings.TrimSpace(token))
	if tokenHash == "" || !IsStrongPassword(password) {
		return models.User{}, envelope.NewError(envelope.InputError, u.i18n.T("user.resetPasswordTokenExpired"), nil)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		u.lo.Error("error hashing verified portal contact password", "error", err)
		return models.User{}, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	tx, err := u.db.Beginx()
	if err != nil {
		u.lo.Error("error beginning customer registration transaction", "error", err)
		return models.User{}, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	defer tx.Rollback()

	var pending pendingCustomerRegistration
	if err := tx.Stmtx(u.q.GetCustomerRegistrationForUpdate).Get(&pending, tokenHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.User{}, envelope.NewError(envelope.InputError, u.i18n.T("user.resetPasswordTokenExpired"), nil)
		}
		u.lo.Error("error loading customer registration", "error", err)
		return models.User{}, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	var (
		state  portalUserState
		userID int
	)
	err = tx.Stmtx(u.q.GetPortalUserForUpdate).Get(&state, pending.Email)
	switch {
	case err == nil:
		if !state.Enabled || state.PortalRegistered {
			return models.User{}, envelope.NewError(envelope.InputError, u.i18n.T("user.resetPasswordTokenExpired"), nil)
		}
		if err := tx.Stmtx(u.q.ActivateExistingPortalUser).Get(
			&userID,
			state.ID,
			pending.FirstName,
			pending.LastName,
			string(passwordHash),
		); err != nil {
			u.lo.Error("error activating customer portal account", "error", err)
			return models.User{}, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
		}
	case errors.Is(err, sql.ErrNoRows):
		if err := tx.Stmtx(u.q.InsertPortalUser).Get(
			&userID,
			pending.Email,
			pending.FirstName,
			pending.LastName,
			string(passwordHash),
		); err != nil {
			u.lo.Error("error creating verified customer portal account", "error", err)
			return models.User{}, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
		}
	default:
		u.lo.Error("error checking customer portal account", "error", err)
		return models.User{}, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	result, err := tx.Stmtx(u.q.DeleteCustomerRegistration).Exec(pending.ID)
	if err != nil {
		u.lo.Error("error consuming customer registration token", "error", err)
		return models.User{}, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return models.User{}, envelope.NewError(envelope.InputError, u.i18n.T("user.resetPasswordTokenExpired"), nil)
	}

	if err := tx.Commit(); err != nil {
		u.lo.Error("error committing customer registration", "error", err)
		return models.User{}, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	return u.Get(userID, "", []string{models.UserTypeContact})
}
