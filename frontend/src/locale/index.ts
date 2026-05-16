import { createI18n } from 'vue-i18n';

export const LOCALE_OPTIONS = [
  { label: '中文', value: 'zh-CN' },
  { label: 'English', value: 'en-US' },
];

// 自动收集所有 locale 文件
function loadLocaleMessages(locale: 'zh-CN' | 'en-US'): Record<string, string> {
  const zhModules = import.meta.glob<{ default: Record<string, string> }>(
    ['./zh-CN/**/*.ts', '../views/**/locale/zh-CN.ts', '../components/**/locale/zh-CN.ts'],
    { eager: true }
  );

  const enModules = import.meta.glob<{ default: Record<string, string> }>(
    ['./en-US/**/*.ts', '../views/**/locale/en-US.ts', '../components/**/locale/en-US.ts'],
    { eager: true }
  );

  const modules = locale === 'zh-CN' ? zhModules : enModules;

  return Object.values(modules).reduce((msgs, mod) => ({ ...msgs, ...mod.default }), {} as Record<string, string>);
}

const defaultLocale = localStorage.getItem('arco-locale') || 'zh-CN';

const i18n = createI18n({
  locale: defaultLocale,
  fallbackLocale: 'en-US',
  legacy: false,
  allowComposition: true,
  messages: {
    'en-US': loadLocaleMessages('en-US'),
    'zh-CN': loadLocaleMessages('zh-CN'),
  },
});

export default i18n;
