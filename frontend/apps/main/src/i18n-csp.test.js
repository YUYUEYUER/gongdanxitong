/* eslint-env es2021 */

import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'

describe('vue-i18n CSP compatibility', () => {
  it('translates runtime messages without the Function constructor', () => {
    const OriginalFunction = globalThis.Function
    globalThis.Function = function blockedFunctionConstructor() {
      throw new Error('unsafe-eval is blocked by CSP')
    }

    try {
      const i18n = createI18n({
        legacy: false,
        locale: 'zh-CN',
        messages: {
          'zh-CN': {
            greeting: '你好，{name}',
          },
        },
      })

      expect(i18n.global.t('greeting', { name: '测试用户' })).toBe('你好，测试用户')
    } finally {
      globalThis.Function = OriginalFunction
    }
  })
})
