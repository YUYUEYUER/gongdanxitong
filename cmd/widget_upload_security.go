package main

import (
	"context"
	"fmt"
	"time"

	"github.com/abhinavxd/libredesk/internal/envelope"
	"github.com/redis/go-redis/v9"
	"github.com/valyala/fasthttp"
)

const (
	maxWidgetUploadsPerContactHour = 20
	maxWidgetUploadsPerInboxHour   = 200
	maxWidgetUploadBytesContactDay = int64(100 << 20)
	maxWidgetUploadBytesInboxDay   = int64(2 << 30)
)

var widgetUploadBudgetScript = redis.NewScript(`
local contact_files = tonumber(redis.call('GET', KEYS[1]) or '0')
local inbox_files = tonumber(redis.call('GET', KEYS[2]) or '0')
local contact_bytes = tonumber(redis.call('GET', KEYS[3]) or '0')
local inbox_bytes = tonumber(redis.call('GET', KEYS[4]) or '0')
local next_bytes = tonumber(ARGV[1])

if contact_files + 1 > tonumber(ARGV[2]) or
   inbox_files + 1 > tonumber(ARGV[3]) or
   contact_bytes + next_bytes > tonumber(ARGV[4]) or
   inbox_bytes + next_bytes > tonumber(ARGV[5]) then
  return 0
end

local cf = redis.call('INCR', KEYS[1])
local inf = redis.call('INCR', KEYS[2])
local cb = redis.call('INCRBY', KEYS[3], next_bytes)
local ib = redis.call('INCRBY', KEYS[4], next_bytes)
if cf == 1 then redis.call('EXPIRE', KEYS[1], tonumber(ARGV[6])) end
if inf == 1 then redis.call('EXPIRE', KEYS[2], tonumber(ARGV[6])) end
if cb == next_bytes then redis.call('EXPIRE', KEYS[3], tonumber(ARGV[7])) end
if ib == next_bytes then redis.call('EXPIRE', KEYS[4], tonumber(ARGV[7])) end
return 1
`)

func checkWidgetUploadBudget(app *App, contactID, inboxID int, nextBytes int64) error {
	if app.redis == nil || contactID <= 0 || inboxID <= 0 || nextBytes <= 0 {
		return widgetUploadBudgetUnavailable(app)
	}
	ctx := app.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().UTC()
	hourBucket := now.Unix() / int64(time.Hour/time.Second)
	dayBucket := now.Unix() / int64(24*time.Hour/time.Second)
	keys := []string{
		fmt.Sprintf("widget_upload:contact:%d:hour:%d", contactID, hourBucket),
		fmt.Sprintf("widget_upload:inbox:%d:hour:%d", inboxID, hourBucket),
		fmt.Sprintf("widget_upload:contact:%d:day:%d:bytes", contactID, dayBucket),
		fmt.Sprintf("widget_upload:inbox:%d:day:%d:bytes", inboxID, dayBucket),
	}
	allowed, err := widgetUploadBudgetScript.Run(ctx, app.redis, keys,
		nextBytes,
		maxWidgetUploadsPerContactHour,
		maxWidgetUploadsPerInboxHour,
		maxWidgetUploadBytesContactDay,
		maxWidgetUploadBytesInboxDay,
		int64((2*time.Hour)/time.Second),
		int64((48*time.Hour)/time.Second),
	).Int()
	if err != nil {
		app.lo.Error("widget upload budget unavailable", "error", err)
		return widgetUploadBudgetUnavailable(app)
	}
	if allowed != 1 {
		return envelope.NewError(envelope.RateLimitError, app.i18n.T("globals.messages.tooManyRequests"), nil)
	}
	return nil
}

func widgetUploadBudgetUnavailable(app *App) error {
	return envelope.NewErrorWithCode(
		envelope.GeneralError,
		fasthttp.StatusServiceUnavailable,
		app.i18n.T("globals.messages.somethingWentWrong"),
		nil,
	)
}
