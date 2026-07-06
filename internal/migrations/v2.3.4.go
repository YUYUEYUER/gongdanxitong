package migrations

import (
	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

func V2_3_4(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf) error {
	_, err := db.Exec(`
		INSERT INTO inboxes (
			channel, config, name, "from", enabled, csat_enabled, prompt_tags_on_reply, secret, linked_email_inbox_id
		)
		SELECT
			'livechat',
			'{
				"brand_name":"客户门户",
				"website_url":"",
				"dark_mode":false,
				"show_powered_by":true,
				"language":"zh-CN",
				"fallback_language":"zh-CN",
				"logo_url":"",
				"launcher":{"position":"right","logo_url":"","color":"#000000","spacing":{"side":20,"bottom":20}},
				"greeting_message":"Hello {{.FirstName | there}}",
				"introduction_message":"How can we help?",
				"chat_introduction":"Ask us anything, or share your feedback.",
				"show_office_hours_in_chat":false,
				"show_office_hours_after_assignment":false,
				"chat_reply_expectation_message":"We typically reply in 5 minutes.",
				"notice_banner":{"enabled":false,"text":"Our response times are slower than usual. We regret the inconvenience caused."},
				"colors":{"primary":"#2563eb"},
				"home_screen":{"header_text_color":"white","background":{"type":"solid","color":"","gradient_start":"#2563eb","gradient_end":"#1e40af","image_url":""},"fade_background":false},
				"features":{"file_upload":true,"emoji":true},
				"continuity":{"offline_threshold":"10m","max_messages_per_email":10,"min_email_interval":"15m"},
				"session_duration":"10h",
				"direct_to_conversation":false,
				"trusted_domains":[],
				"blocked_ips":[],
				"home_apps":[],
				"visitors":{"start_conversation_button_text":"Start conversation","allow_start_conversation":true,"prevent_multiple_conversations":false,"prevent_reply_to_closed_conversation":false},
				"users":{"start_conversation_button_text":"Start conversation","allow_start_conversation":true,"prevent_multiple_conversations":false,"prevent_reply_to_closed_conversation":false},
				"prechat_form":{"enabled":false,"title":"","fields":[]}
			}'::jsonb,
			'客户门户',
			'',
			true,
			false,
			false,
			md5(random()::text || clock_timestamp()::text),
			NULL
		WHERE NOT EXISTS (
			SELECT 1 FROM inboxes WHERE deleted_at IS NULL AND channel = 'livechat' AND name = '客户门户'
		);
	`)
	return err
}
