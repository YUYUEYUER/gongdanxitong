package main

import (
	"strings"
	"testing"

	fsessionmodels "github.com/abhinavxd/libredesk/internal/auth/models"
	"github.com/abhinavxd/libredesk/internal/authz"
	authzmodels "github.com/abhinavxd/libredesk/internal/authz/models"
	"github.com/abhinavxd/libredesk/internal/envelope"
	umodels "github.com/abhinavxd/libredesk/internal/user/models"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/logf"
)

func TestCustomerTicketLimitsRejectBeforePersistence(t *testing.T) {
	app := testSecurityApp(t)

	createReq := testFastRequest(app, fasthttp.MethodPost, "application/json")
	createReq.RequestCtx.SetUserValue("user", fsessionmodels.User{ID: 1})
	createReq.RequestCtx.Request.SetBodyString(`{"subject":"` + strings.Repeat("s", maxCustomerTicketSubjectLength+1) + `","content":"message"}`)
	require.NoError(t, handleCustomerCreateTicket(createReq))
	require.Equal(t, fasthttp.StatusBadRequest, createReq.RequestCtx.Response.StatusCode())

	replyReq := testFastRequest(app, fasthttp.MethodPost, "application/json")
	replyReq.RequestCtx.SetUserValue("user", fsessionmodels.User{ID: 1})
	replyReq.RequestCtx.SetUserValue("uuid", "conversation-uuid")
	replyReq.RequestCtx.Request.SetBodyString(`{"message":"` + strings.Repeat("m", maxCustomerTicketContentLength+1) + `"}`)
	require.NoError(t, handleCustomerReplyTicket(replyReq))
	require.Equal(t, fasthttp.StatusBadRequest, replyReq.RequestCtx.Response.StatusCode())
}

func TestWriteAsContactPermissionIsExplicit(t *testing.T) {
	app := testSecurityApp(t)
	logger := logf.New(logf.Opts{})
	enforcer, err := authz.NewEnforcer(&logger, app.i18n)
	require.NoError(t, err)
	app.authz = enforcer

	err = enforceAgentPermission(app, umodels.User{}, authzmodels.PermMessagesWriteAsContact)
	requireEnvelopeError(t, err, envelope.PermissionError, fasthttp.StatusForbidden)

	user := umodels.User{Permissions: []string{authzmodels.PermMessagesWriteAsContact}}
	require.NoError(t, enforceAgentPermission(app, user, authzmodels.PermMessagesWriteAsContact))
}
