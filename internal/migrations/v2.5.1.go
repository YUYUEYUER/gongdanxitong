package migrations

import (
	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

func V2_5_1(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf) error {
	_, err := db.Exec(`
		INSERT INTO templates ("type", body, is_default, "name", subject, is_builtin)
		SELECT
			'email_outgoing'::template_type,
			$email_reply_template$
<div style="display:none; max-height:0; overflow:hidden; opacity:0; color:transparent;">
  {{ if .IsContinuityEmail }}客户在聊天结束后继续回复了工单。{{ else }}客服已回复您的工单，请进入工单系统查看并回复。{{ end }}
</div>

<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="width:100%; background:#f6f1ea; margin:0; padding:28px 12px; font-family:Arial, Helvetica, sans-serif; color:#1f2937;">
  <tr>
    <td align="center">
      <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="width:100%; max-width:640px; margin:0 auto;">
        <tr>
          <td style="padding:0 0 14px;">
            <table role="presentation" width="100%" cellpadding="0" cellspacing="0">
              <tr>
                <td align="left" style="vertical-align:middle;">
                  {{ if LogoURL }}
                  <img src="{{ LogoURL }}" alt="{{ html SiteName }}" height="32" style="display:block; max-height:32px; border:0;">
                  {{ else }}
                  <div style="font-size:18px; line-height:24px; font-weight:700; color:#111827;">{{ if SiteName }}{{ html SiteName }}{{ else }}LYA 工单系统{{ end }}</div>
                  {{ end }}
                </td>
                <td align="right" style="vertical-align:middle; font-size:12px; line-height:18px; color:#8b5e34;">
                  工单 #{{ html .Conversation.ReferenceNumber }}
                </td>
              </tr>
            </table>
          </td>
        </tr>

        <tr>
          <td style="background:#fffdf8; border:1px solid #eadfce; border-radius:8px; overflow:hidden;">
            <table role="presentation" width="100%" cellpadding="0" cellspacing="0">
              <tr>
                <td style="padding:26px 28px 18px;">
                  <div style="font-size:13px; line-height:18px; color:#8b5e34; font-weight:700; text-transform:uppercase;">
                    {{ if .IsContinuityEmail }}聊天延续邮件{{ else }}客服回复{{ end }}
                  </div>
                  <h1 style="margin:8px 0 8px; font-size:22px; line-height:30px; font-weight:700; color:#111827;">
                    {{ if .Conversation.Subject }}{{ html .Conversation.Subject }}{{ else }}您的工单有新回复{{ end }}
                  </h1>
                  <p style="margin:0; font-size:14px; line-height:22px; color:#6b7280;">
                    {{ if .Contact.FullName }}{{ html .Contact.FullName }}，您好。{{ else }}您好。{{ end }}我们已经更新了您的工单。{{ if RootURL }}请点击下方按钮进入工单系统查看并回复。{{ else }}请登录工单系统查看并继续回复。{{ end }}
                  </p>
                </td>
              </tr>
              <tr>
                <td style="padding:0 28px 6px;">
                  <div style="height:1px; line-height:1px; background:#eadfce;">&nbsp;</div>
                </td>
              </tr>
              <tr>
                <td style="padding:22px 28px 12px; font-size:15px; line-height:24px; color:#1f2937;">
                  {{ template "content" . }}
                </td>
              </tr>
              {{ if RootURL }}
              <tr>
                <td style="padding:14px 28px 26px;">
                  <a href="{{ RootURL }}/portal/tickets/{{ .Conversation.UUID }}" style="display:inline-block; background:#1f2937; color:#ffffff; text-decoration:none; font-size:14px; line-height:20px; font-weight:700; padding:11px 18px; border-radius:6px;">查看并回复工单</a>
                </td>
              </tr>
              {{ end }}
            </table>
          </td>
        </tr>

        <tr>
          <td style="padding:16px 4px 0; font-size:12px; line-height:19px; color:#7c6f64;">
            如需补充信息，请登录工单系统继续回复工单 #{{ html .Conversation.ReferenceNumber }}。本邮件仅用于通知，请勿转发包含敏感信息的内容。
          </td>
        </tr>
        <tr>
          <td style="padding:8px 4px 0; font-size:12px; line-height:18px; color:#a09082;">
            {{ if SiteName }}{{ html SiteName }}{{ else }}LYA 工单系统{{ end }}
          </td>
        </tr>
      </table>
    </td>
  </tr>
</table>
$email_reply_template$,
			true,
			'LYA ticket reply',
			NULL,
			false
		WHERE NOT EXISTS (
			SELECT 1 FROM templates WHERE is_default IS TRUE
		);
	`)
	return err
}
