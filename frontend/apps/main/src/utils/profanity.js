const blockedTerms = [
  'fuck',
  'fucking',
  'motherfucker',
  'shit',
  'bullshit',
  'bitch',
  'asshole',
  'bastard',
  'cunt',
  'whore',
  'slut',
  'dickhead',
  'nmsl',
  'cnm',
  'tmd',
  'tmdb',
  'shabi',
  'sabi',
  '\u50bb\u903c',
  '\u50bbx',
  '\u50bb\u53c9',
  '\u50bb\u7f3a',
  '\u50bbb',
  '\u50bb\u6bd4',
  '\u50bb\u6279',
  '\u715e\u7b14',
  '\u5c3c\u739b',
  '\u6c99\u6bd4',
  '\u9ebb\u6279',
  '\u9ebb\u9ebb\u6279',
  '\u5988\u5356\u6279',
  '\u5988\u4e86\u4e2a\u903c',
  '\u5988\u4e2a\u9e21',
  '\u8111\u6b8b',
  '\u8111\u761b',
  '\u8111\u762b',
  '\u5f31\u667a',
  '\u667a\u969c',
  '\u87ba\u969c',
  '\u87ba\u7634',
  '\u5e9f\u7269',
  '\u5783\u573e',
  '\u72d7\u4e1c\u897f',
  '\u64cd\u4f60\u5988',
  '\u64cd\u4f60\u5a18',
  '\u8349\u4f60\u7239',
  '\u8349\u4f60\u5988',
  '\u8349\u4f60\u5a18',
  '\u8349\u4f60\u5927\u7237',
  '\u8349\u6ce5\u9a6c',
  '\u8349\u62df\u5417',
  '\u64cd\u62df\u5417',
  '\u53bb\u4f60\u5988',
  '\u65e5\u4f60\u5988',
  '\u4f60\u5988\u7684',
  '\u4ed6\u5988\u7684',
  '\u4ed6\u5988',
  '\u5988\u7684',
  '\u5988\u903c',
  '\u5988p',
  '\u9a9a\u8d27',
  '\u6eda\u4f60\u5988',
  '\u738b\u516b\u86cb',
  '\u72d7\u65e5\u7684',
  '\u8d31\u4eba',
  '\u5a4a\u5b50',
  '\u6b7b\u5168\u5bb6',
  '\u6b7b\u5988',
  '\u6b7b\u7239',
  '\u53bb\u6b7b'
]

const leetMap = {
  '@': 'a',
  '$': 's',
  '0': 'o',
  '1': 'i',
  '3': 'e',
  '4': 'a',
  '5': 's',
  '7': 't'
}

export function normalizeModerationText (text = '') {
  const mapped = text
    .toLowerCase()
    .split('')
    .map((char) => leetMap[char] || char)
    .join('')

  return mapped.replace(/[^\p{L}\p{N}]+/gu, '')
}

export function findBlockedLanguage (...values) {
  for (const value of values) {
    const normalized = normalizeModerationText(value)
    if (!normalized) continue

    for (const term of blockedTerms) {
      if (normalized.includes(normalizeModerationText(term))) {
        return term
      }
    }
  }

  return ''
}
