import { errorDetails, errorKey } from '@/api/client'

// Keys spelled out for the i18n key check.
const passwordReasons: Record<string, (t: (key: string) => string) => string> = {
  too_short: (t) => t('password.too_short'),
  too_long: (t) => t('password.too_long'),
  too_common: (t) => t('password.too_common'),
}

/** A failed request as an i18n key for the form and messages per field path. */
export interface FormError {
  key: string
  fields: Record<string, string>
}

/**
 * Splits an API error into the form message and field messages. Password reasons are
 * i18n keys (`password.too_short`); configuration problems come as server text.
 */
export function formError(err: unknown, t: (key: string) => string): FormError {
  const fields: Record<string, string> = {}
  for (const d of errorDetails(err)) {
    const reason = d.path === 'password' ? passwordReasons[d.message] : undefined
    const text = reason ? reason(t) : d.message
    fields[d.path] = fields[d.path] ? `${fields[d.path]}; ${text}` : text
  }
  return { key: errorKey(err), fields }
}
