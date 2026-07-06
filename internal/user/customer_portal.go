package user

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/abhinavxd/libredesk/internal/envelope"
	"github.com/abhinavxd/libredesk/internal/user/models"
	"golang.org/x/crypto/bcrypt"
)

const customerPortalRegisteredKey = "portal_registered"

func IsCustomerPortalRegistered(user models.User) bool {
	if len(user.CustomAttributes) == 0 {
		return false
	}

	var attrs map[string]any
	if err := json.Unmarshal(user.CustomAttributes, &attrs); err != nil {
		return false
	}

	registered, _ := attrs[customerPortalRegisteredKey].(bool)
	return registered
}

func (u *Manager) VerifyContactPassword(email string, password []byte) (models.User, error) {
	var user models.User

	user, err := u.Get(0, strings.TrimSpace(strings.ToLower(email)), []string{models.UserTypeContact})
	if err != nil {
		if envErr, ok := err.(envelope.Error); ok && envErr.ErrorType == envelope.NotFoundError {
			return user, envelope.NewError(envelope.InputError, u.i18n.T("user.invalidEmailPassword"), nil)
		}
		return user, err
	}

	if !IsCustomerPortalRegistered(user) {
		return user, envelope.NewError(envelope.InputError, u.i18n.T("user.invalidEmailPassword"), nil)
	}

	if err := u.verifyPassword(password, user.Password.String); err != nil {
		return user, envelope.NewError(envelope.InputError, u.i18n.T("user.invalidEmailPassword"), nil)
	}

	return user, nil
}

func (u *Manager) RegisterCustomerContact(firstName, lastName, email, password string) (models.User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	firstName = strings.TrimSpace(firstName)
	lastName = strings.TrimSpace(lastName)

	if !IsStrongPassword(password) {
		return models.User{}, envelope.NewError(envelope.InputError, PasswordHint, nil)
	}

	blocked, err := u.IsEmailBlocked(email)
	if err != nil {
		return models.User{}, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	if blocked {
		return models.User{}, envelope.NewError(envelope.PermissionError, u.i18n.T("user.accountDisabled"), nil)
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		u.lo.Error("error hashing portal contact password", "error", err)
		return models.User{}, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	var user models.User

	user, err = u.GetContactByEmail(email)
	switch {
	case err == nil:
		if IsCustomerPortalRegistered(user) {
			return models.User{}, envelope.NewError(envelope.ConflictError, u.i18n.T("customerAuth.emailAlreadyRegistered"), nil)
		}
	case !isNotFoundError(err):
		return models.User{}, err
	default:
		visitor, visitorErr := u.GetVisitorByEmail(email)
		if visitorErr == nil {
			if upgradeErr := u.UpgradeVisitorToContact(visitor.ID); upgradeErr != nil {
				return models.User{}, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
			}
			user, err = u.Get(visitor.ID, "", []string{models.UserTypeContact})
			if err != nil {
				return models.User{}, err
			}
		} else if !isNotFoundError(visitorErr) {
			return models.User{}, visitorErr
		}
	}

	return u.upsertPortalContactUser(user, firstName, lastName, email, string(passwordHash))
}

func (u *Manager) upsertPortalContactUser(existing models.User, firstName, lastName, email, passwordHash string) (models.User, error) {
	var (
		user = existing
		err  error
	)

	if user.ID == 0 {
		user = models.User{
			FirstName: firstName,
			LastName:  lastName,
		}
		user.Email.String = email
		user.Email.Valid = email != ""
		if err := u.CreateContact(&user); err != nil {
			return models.User{}, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
		}
	}

	if err := u.UpdateContactBasicInfo(user.ID, firstName, lastName, email); err != nil {
		return models.User{}, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	if _, err = u.q.SetUserPassword.Exec(passwordHash, user.ID); err != nil {
		u.lo.Error("error setting portal contact password", "contact_id", user.ID, "error", err)
		return models.User{}, envelope.NewError(envelope.GeneralError, u.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	if err := u.SaveCustomAttributes(user.ID, map[string]any{customerPortalRegisteredKey: true}, false); err != nil {
		return models.User{}, err
	}

	return u.Get(user.ID, "", []string{models.UserTypeContact})
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	var envErr envelope.Error
	return errors.As(err, &envErr) && envErr.ErrorType == envelope.NotFoundError
}
