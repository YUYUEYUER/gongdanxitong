package oidc

import (
	"database/sql"
	"embed"
	"fmt"
	"strings"

	"github.com/abhinavxd/libredesk/internal/crypto"
	"github.com/abhinavxd/libredesk/internal/dbutil"
	"github.com/abhinavxd/libredesk/internal/envelope"
	"github.com/abhinavxd/libredesk/internal/oidc/models"
	"github.com/jmoiron/sqlx"
	"github.com/knadh/go-i18n"
	"github.com/lib/pq"
	"github.com/zerodha/logf"
)

var (
	//go:embed queries.sql
	efs         embed.FS
	redirectURL = "/api/v1/oidc/%d/finish"
)

// Manager handles oidc-related operations.
type Manager struct {
	q             queries
	lo            *logf.Logger
	i18n          *i18n.I18n
	setting       settingsStore
	encryptionKey string
	db            *sqlx.DB
}

// Opts contains options for initializing the Manager.
type Opts struct {
	DB            *sqlx.DB
	Lo            *logf.Logger
	I18n          *i18n.I18n
	EncryptionKey string
}

// queries contains prepared SQL queries.
type queries struct {
	GetAllOIDC                 *sqlx.Stmt `query:"get-all-oidc"`
	GetOIDC                    *sqlx.Stmt `query:"get-oidc"`
	InsertOIDC                 *sqlx.Stmt `query:"insert-oidc"`
	UpdateOIDC                 *sqlx.Stmt `query:"update-oidc"`
	DeleteOIDC                 *sqlx.Stmt `query:"delete-oidc"`
	LockOIDCForDelete          *sqlx.Stmt `query:"lock-oidc-for-delete"`
	GetOIDCIdentityUserIDs     *sqlx.Stmt `query:"get-oidc-identity-user-ids"`
	RevokeOIDCIdentitySessions *sqlx.Stmt `query:"revoke-oidc-identity-sessions"`
	ResolveOIDCUserIdentity    *sqlx.Stmt `query:"resolve-oidc-user-identity"`
	BindOIDCUserIdentity       *sqlx.Stmt `query:"bind-oidc-user-identity"`
}

type settingsStore interface {
	GetAppRootURL() (string, error)
}

// New creates and returns a new instance of the oidc Manager.
func New(opts Opts, setting settingsStore) (*Manager, error) {
	var q queries
	if err := dbutil.ScanSQLFile("queries.sql", &q, opts.DB, efs); err != nil {
		return nil, err
	}
	return &Manager{
		q:             q,
		lo:            opts.Lo,
		i18n:          opts.I18n,
		setting:       setting,
		encryptionKey: opts.EncryptionKey,
		db:            opts.DB,
	}, nil
}

// Get returns an oidc by id.
func (o *Manager) Get(id int) (models.OIDC, error) {
	var oidc models.OIDC
	if err := o.q.GetOIDC.Get(&oidc, id); err != nil {
		if err == sql.ErrNoRows {
			return oidc, envelope.NewError(envelope.NotFoundError, o.i18n.T("validation.notFoundOidcProvider"), nil)
		}

		o.lo.Error("error fetching oidc", "error", err)
		return oidc, envelope.NewError(envelope.GeneralError, o.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	o.decryptOIDC(&oidc)

	oidc.SetProviderLogo()
	rootURL, err := o.setting.GetAppRootURL()
	if err != nil {
		return models.OIDC{}, err
	}
	oidc.RedirectURI = fmt.Sprintf(rootURL+redirectURL, oidc.ID)
	return oidc, nil
}

// GetAll retrieves all oidc.
func (o *Manager) GetAll() ([]models.OIDC, error) {
	var oidc = make([]models.OIDC, 0)
	if err := o.q.GetAllOIDC.Select(&oidc); err != nil {
		o.lo.Error("error fetching oidc", "error", err)
		return oidc, envelope.NewError(envelope.GeneralError, o.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	// Get root URL of the app.
	rootURL, err := o.setting.GetAppRootURL()
	if err != nil {
		return nil, err
	}

	o.decryptOIDCSlice(oidc)

	// Set logo and redirect URL for each record
	for i := range oidc {
		oidc[i].RedirectURI = fmt.Sprintf(rootURL+redirectURL, oidc[i].ID)
		oidc[i].SetProviderLogo()
	}
	return oidc, nil
}

// Create adds a new oidc.
func (o *Manager) Create(oidc models.OIDC) (models.OIDC, error) {
	// Encrypt sensitive fields before saving
	encryptedClientID, encryptedClientSecret, err := o.encryptOIDC(oidc.ClientID, oidc.ClientSecret)
	if err != nil {
		return models.OIDC{}, envelope.NewError(envelope.GeneralError, o.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	var createdOIDC models.OIDC
	if err := o.q.InsertOIDC.Get(&createdOIDC, oidc.Name, oidc.Provider, oidc.ProviderURL, encryptedClientID, encryptedClientSecret, oidc.LogoURL); err != nil {
		o.lo.Error("error inserting oidc", "error", err)
		return models.OIDC{}, envelope.NewError(envelope.GeneralError, o.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	o.decryptOIDC(&createdOIDC)

	return createdOIDC, nil
}

// Update updates a oidc by id.
func (o *Manager) Update(id int, oidc models.OIDC) (models.OIDC, error) {
	updated, _, err := o.UpdateAndRevoke(id, oidc)
	return updated, err
}

// UpdateAndRevoke updates a provider and atomically revokes every identity
// linked through it when a trust-boundary setting changes.
func (o *Manager) UpdateAndRevoke(id int, oidc models.OIDC) (models.OIDC, []int, error) {
	current, err := o.Get(id)
	if err != nil {
		return models.OIDC{}, nil, err
	}

	// If client secret is not provided, use the current one (already decrypted from Get)
	if oidc.ClientSecret == "" {
		oidc.ClientSecret = current.ClientSecret
	}

	// Encrypt sensitive fields before updating
	encryptedClientID, encryptedClientSecret, err := o.encryptOIDC(oidc.ClientID, oidc.ClientSecret)
	if err != nil {
		return models.OIDC{}, nil, envelope.NewError(envelope.GeneralError, o.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	revoke := oidcSecurityBoundaryChanged(current, oidc)

	tx, err := o.db.Beginx()
	if err != nil {
		o.lo.Error("error beginning OIDC update", "error", err)
		return models.OIDC{}, nil, envelope.NewError(envelope.GeneralError, o.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	defer tx.Rollback()

	var updatedOIDC models.OIDC
	if err := tx.Stmtx(o.q.UpdateOIDC).Get(&updatedOIDC, id, oidc.Name, oidc.Provider, oidc.ProviderURL, encryptedClientID, encryptedClientSecret, oidc.Enabled, oidc.LogoURL); err != nil {
		o.lo.Error("error updating oidc", "error", err)
		return models.OIDC{}, nil, envelope.NewError(envelope.GeneralError, o.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	var userIDs []int
	if revoke {
		if err := tx.Stmtx(o.q.GetOIDCIdentityUserIDs).Select(&userIDs, id); err != nil {
			o.lo.Error("error fetching OIDC-linked users during update", "error", err)
			return models.OIDC{}, nil, envelope.NewError(envelope.GeneralError, o.i18n.T("globals.messages.somethingWentWrong"), nil)
		}
		if len(userIDs) > 0 {
			if _, err := tx.Stmtx(o.q.RevokeOIDCIdentitySessions).Exec(pq.Array(userIDs)); err != nil {
				o.lo.Error("error revoking OIDC-linked sessions during update", "error", err)
				return models.OIDC{}, nil, envelope.NewError(envelope.GeneralError, o.i18n.T("globals.messages.somethingWentWrong"), nil)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		o.lo.Error("error committing OIDC update", "error", err)
		return models.OIDC{}, nil, envelope.NewError(envelope.GeneralError, o.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	o.decryptOIDC(&updatedOIDC)

	return updatedOIDC, userIDs, nil
}

func oidcSecurityBoundaryChanged(current, next models.OIDC) bool {
	return current.Enabled != next.Enabled ||
		strings.TrimSpace(current.ProviderURL) != strings.TrimSpace(next.ProviderURL) ||
		current.Provider != next.Provider ||
		current.ClientID != next.ClientID ||
		current.ClientSecret != next.ClientSecret
}

// Delete revokes all sessions linked through the provider and deletes it in one transaction.
func (o *Manager) Delete(id int) ([]int, error) {
	tx, err := o.db.Beginx()
	if err != nil {
		o.lo.Error("error beginning OIDC deletion", "error", err)
		return nil, envelope.NewError(envelope.GeneralError, o.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	defer tx.Rollback()
	var lockedProviderID int
	if err := tx.Stmtx(o.q.LockOIDCForDelete).Get(&lockedProviderID, id); err != nil {
		if err == sql.ErrNoRows {
			return nil, envelope.NewError(envelope.NotFoundError, o.i18n.T("validation.notFoundOidcProvider"), nil)
		}
		o.lo.Error("error locking OIDC provider for deletion", "error", err)
		return nil, envelope.NewError(envelope.GeneralError, o.i18n.T("globals.messages.somethingWentWrong"), nil)
	}

	var userIDs []int
	if err := tx.Stmtx(o.q.GetOIDCIdentityUserIDs).Select(&userIDs, id); err != nil {
		o.lo.Error("error fetching OIDC-linked users", "error", err)
		return nil, envelope.NewError(envelope.GeneralError, o.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	if len(userIDs) > 0 {
		if _, err := tx.Stmtx(o.q.RevokeOIDCIdentitySessions).Exec(pq.Array(userIDs)); err != nil {
			o.lo.Error("error revoking OIDC-linked sessions", "error", err)
			return nil, envelope.NewError(envelope.GeneralError, o.i18n.T("globals.messages.somethingWentWrong"), nil)
		}
	}
	if _, err := tx.Stmtx(o.q.DeleteOIDC).Exec(id); err != nil {
		o.lo.Error("error deleting oidc", "error", err)
		return nil, envelope.NewError(envelope.GeneralError, o.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	if err := tx.Commit(); err != nil {
		o.lo.Error("error committing OIDC deletion", "error", err)
		return nil, envelope.NewError(envelope.GeneralError, o.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return userIDs, nil
}

// ResolveUserIdentity resolves a previously linked issuer and subject pair.
func (o *Manager) ResolveUserIdentity(issuer, subject string) (int, bool, error) {
	var userID int
	if err := o.q.ResolveOIDCUserIdentity.Get(&userID, issuer, subject); err != nil {
		if err == sql.ErrNoRows {
			return 0, false, nil
		}
		o.lo.Error("error resolving OIDC identity", "error", err)
		return 0, false, envelope.NewError(envelope.GeneralError, o.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return userID, true, nil
}

// BindUserIdentity links an OIDC identity to an existing agent.
func (o *Manager) BindUserIdentity(providerID int, issuer, subject string, userID int, email string) (int, error) {
	var boundUserID int
	if err := o.q.BindOIDCUserIdentity.Get(&boundUserID, providerID, issuer, subject, userID, email); err != nil {
		o.lo.Error("error binding OIDC identity", "error", err)
		return 0, envelope.NewError(envelope.GeneralError, o.i18n.T("globals.messages.somethingWentWrong"), nil)
	}
	return boundUserID, nil
}

// encryptOIDC encrypts sensitive OIDC fields (ClientID and ClientSecret).
// Returns the encrypted values and any error encountered.
func (o *Manager) encryptOIDC(clientID, clientSecret string) (encClientID, encClientSecret string, err error) {
	encClientID, err = crypto.Encrypt(clientID, o.encryptionKey)
	if err != nil {
		o.lo.Error("error encrypting client_id", "error", err)
		return "", "", err
	}

	encClientSecret, err = crypto.Encrypt(clientSecret, o.encryptionKey)
	if err != nil {
		o.lo.Error("error encrypting client_secret", "error", err)
		return "", "", err
	}

	return encClientID, encClientSecret, nil
}

// Decrypt failures clear the field so the app stays usable across encryption_key rotation.
func (o *Manager) decryptOIDC(oidc *models.OIDC) {
	if oidc.ClientID != "" {
		decrypted, err := crypto.Decrypt(oidc.ClientID, o.encryptionKey)
		if err != nil {
			o.lo.Error("error decrypting client_id, clearing field", "error", err, "oidc_id", oidc.ID)
			oidc.ClientID = ""
		} else {
			oidc.ClientID = decrypted
		}
	}

	if oidc.ClientSecret != "" {
		decrypted, err := crypto.Decrypt(oidc.ClientSecret, o.encryptionKey)
		if err != nil {
			o.lo.Error("error decrypting client_secret, clearing field", "error", err, "oidc_id", oidc.ID)
			oidc.ClientSecret = ""
		} else {
			oidc.ClientSecret = decrypted
		}
	}
}

func (o *Manager) decryptOIDCSlice(oidcs []models.OIDC) {
	for i := range oidcs {
		o.decryptOIDC(&oidcs[i])
	}
}
