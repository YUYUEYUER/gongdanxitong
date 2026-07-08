import * as z from 'zod'
import { isGoDuration, validateEmail, isValidTemplate } from '@shared-ui/utils/string'
import {
  AUTH_TYPE_PASSWORD,
  AUTH_TYPE_OAUTH2,
  OUTBOUND_PROVIDER_RESEND,
  OUTBOUND_PROVIDER_SMTP
} from '@main/constants/auth.js'

const FROM_NAME_TEMPLATE_VARS = ['.Agent.FirstName', '.Agent.LastName', '.Agent.FullName', '.Inbox.Name']

export const createFormSchema = (t) => z.object({
  name: z.string().min(1, t('globals.messages.required')),
  from: z.string().min(1, t('globals.messages.required')),
  from_name_template: z
    .string()
    .optional()
    .default('')
    .refine((val) => isValidTemplate(val, FROM_NAME_TEMPLATE_VARS), {
      message: t('admin.inbox.fromNameTemplate.invalidTemplate')
    }),
  reply_to: z
    .string()
    .optional()
    .refine((v) => !v || validateEmail(v), {
      message: t('validation.invalidEmail')
    }),
  enabled: z.boolean().optional(),
  csat_enabled: z.boolean().optional(),
  prompt_tags_on_reply: z.boolean().optional(),
  enable_plus_addressing: z.boolean().optional(),
  auth_type: z.enum([AUTH_TYPE_PASSWORD, AUTH_TYPE_OAUTH2]),
  outbound_provider: z.enum([OUTBOUND_PROVIDER_SMTP, OUTBOUND_PROVIDER_RESEND]).optional(),
  oauth: z.object({
    access_token: z.string().optional(),
    client_id: z.string().optional(),
    client_secret: z.string().optional(),
    expires_at: z.string().optional(),
    provider: z.string().optional(),
    refresh_token: z.string().optional()
  }).optional(),
  imap: z.object({
    host: z.string().min(1, t('globals.messages.required')),
    port: z.number().min(1).max(65535),
    mailbox: z.string().min(1, t('globals.messages.required')),
    username: z.string().min(1, t('globals.messages.required')),
    password: z.string().min(1, t('globals.messages.required')),
    tls_type: z.enum(['none', 'starttls', 'tls']),
    tls_skip_verify: z.boolean().optional(),
    scan_inbox_since: z.string().min(1, t('globals.messages.required')).refine(isGoDuration, {
      message: t('validation.invalidDuration')
    }),
    read_interval: z.string().min(1, t('globals.messages.required')).refine(isGoDuration, {
      message: t('validation.invalidDuration')
    })
  }),
  resend: z.object({
    api_key: z.string().optional(),
    api_url: z.string().optional()
  }).optional(),
  smtp: z.object({
    host: z.string().optional(),
    port: z.number().min(1).max(65535).optional(),
    username: z.string().optional(),
    password: z.string().optional(),
    max_conns: z.number().min(1).optional(),
    max_msg_retries: z.number().min(0).max(100).optional(),
    idle_timeout: z.string().optional().refine((v) => !v || isGoDuration(v), {
      message: t('validation.invalidDuration')
    }),
    pool_wait_timeout: z.string().optional().refine((v) => !v || isGoDuration(v), {
      message: t('validation.invalidDuration')
    }),
    tls_type: z.enum(['none', 'starttls', 'tls']),
    tls_skip_verify: z.boolean().optional(),
    hello_hostname: z.string().optional(),
    auth_protocol: z.enum(['login', 'cram', 'plain', 'none'])
  }).optional()
}).superRefine((data, ctx) => {
  const outboundProvider = data.outbound_provider || OUTBOUND_PROVIDER_SMTP
  if (outboundProvider === OUTBOUND_PROVIDER_RESEND && !data.resend?.api_key) {
    ctx.addIssue({
      code: z.ZodIssueCode.custom,
      path: ['resend', 'api_key'],
      message: t('globals.messages.required')
    })
  }
  if (outboundProvider === OUTBOUND_PROVIDER_SMTP && !data.smtp) {
    ctx.addIssue({
      code: z.ZodIssueCode.custom,
      path: ['smtp'],
      message: t('globals.messages.required')
    })
  }
  if (outboundProvider === OUTBOUND_PROVIDER_SMTP && data.smtp) {
    const requiredFields = ['host', 'port', 'username', 'password', 'max_conns', 'max_msg_retries', 'idle_timeout', 'pool_wait_timeout']
    requiredFields.forEach((field) => {
      if (data.smtp[field] === undefined || data.smtp[field] === '') {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['smtp', field],
          message: t('globals.messages.required')
        })
      }
    })
  }
})
