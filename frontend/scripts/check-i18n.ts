/* eslint-disable no-console, no-restricted-syntax, no-continue */
/**
 * i18n 键一致性检查脚本
 * 用法: npx tsx scripts/check-i18n.ts
 *
 * 检查项:
 * 1. zh-CN 有但 en-US 缺失的键（未翻译）
 * 2. en-US 有但 zh-CN 缺失的键（孤立翻译）
 * 3. 代码中使用但 locale 中缺失的键
 */
import { readdirSync, readFileSync, statSync } from 'fs';
import { join, resolve } from 'path';

const ROOT = resolve(__dirname, '../src');

// 递归收集所有匹配的文件
function collectFiles(dir: string, pattern: RegExp): string[] {
  const results: string[] = [];
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) {
      results.push(...collectFiles(full, pattern));
    } else if (pattern.test(full)) {
      results.push(full);
    }
  }
  return results;
}

function extractKeysFromFile(filePath: string, keys: Set<string>) {
  const content = readFileSync(filePath, 'utf-8');
  // 匹配 'key.name': 或 "key.name":
  const regex = /['"]([a-zA-Z][a-zA-Z0-9._-]+)['"]\s*:/g;
  let match = regex.exec(content);
  while (match) {
    keys.add(match[1]);
    match = regex.exec(content);
  }
}

// 从 locale 文件中提取所有键
function extractKeysFromLocaleFiles(lang: string): Set<string> {
  const keys = new Set<string>();
  const localeDir = join(ROOT, 'locale', lang);

  // src/locale/{lang}/*.ts
  if (statSync(localeDir).isDirectory()) {
    for (const file of collectFiles(localeDir, /\.ts$/)) {
      extractKeysFromFile(file, keys);
    }
  }

  // src/views/**/locale/{lang}.ts
  const viewsDir = join(ROOT, 'views');
  for (const file of collectFiles(viewsDir, new RegExp(`locale/${lang}\\.ts$`))) {
    extractKeysFromFile(file, keys);
  }

  // src/components/**/locale/{lang}.ts
  const componentsDir = join(ROOT, 'components');
  for (const file of collectFiles(componentsDir, new RegExp(`locale/${lang}\\.ts$`))) {
    extractKeysFromFile(file, keys);
  }

  return keys;
}

// 从源码中提取使用的 i18n 键
function extractUsedKeys(): Set<string> {
  const keys = new Set<string>();
  const sourceFiles = [
    ...collectFiles(join(ROOT, 'views'), /\.(vue|ts|tsx)$/),
    ...collectFiles(join(ROOT, 'components'), /\.(vue|ts|tsx)$/),
    ...collectFiles(join(ROOT, 'layout'), /\.(vue|ts|tsx)$/),
  ];

  for (const file of sourceFiles) {
    // 跳过 locale 文件本身
    if (/locale\/(zh-CN|en-US)/.test(file)) continue;

    const content = readFileSync(file, 'utf-8');
    // 匹配 $t('key.name') t('key.name') — 键必须包含至少一个点号
    const regex = /\$?t\(\s*['"]([a-zA-Z][a-zA-Z0-9_]*\.[a-zA-Z0-9._-]+)['"]/g;
    let match = regex.exec(content);
    while (match) {
      keys.add(match[1]);
      match = regex.exec(content);
    }
  }

  return keys;
}

// 主逻辑
function main() {
  const zhKeys = extractKeysFromLocaleFiles('zh-CN');
  const enKeys = extractKeysFromLocaleFiles('en-US');
  const usedKeys = extractUsedKeys();

  let hasError = false;

  // 1. zh-CN 有但 en-US 缺失
  const missingInEn = [...zhKeys].filter((k) => !enKeys.has(k));
  if (missingInEn.length > 0) {
    hasError = true;
    console.log('\n❌ zh-CN 中存在但 en-US 缺失的键（未翻译）:');
    missingInEn.forEach((k) => console.log(`   - ${k}`));
  }

  // 2. en-US 有但 zh-CN 缺失
  const missingInZh = [...enKeys].filter((k) => !zhKeys.has(k));
  if (missingInZh.length > 0) {
    hasError = true;
    console.log('\n❌ en-US 中存在但 zh-CN 缺失的键（孤立翻译）:');
    missingInZh.forEach((k) => console.log(`   - ${k}`));
  }

  // 3. 代码中使用但 locale 中缺失
  const allKeys = new Set([...zhKeys, ...enKeys]);
  const missingInLocale = [...usedKeys].filter((k) => !allKeys.has(k));
  if (missingInLocale.length > 0) {
    hasError = true;
    console.log('\n❌ 代码中使用但 locale 文件中缺失的键:');
    missingInLocale.forEach((k) => console.log(`   - ${k}`));
  }

  // 4. locale 中定义但代码中未使用（仅警告）
  const unusedKeys = [...allKeys].filter((k) => !usedKeys.has(k));
  if (unusedKeys.length > 0) {
    console.log(`\n⚠️  locale 中定义但代码中未直接引用的键 (${unusedKeys.length} 个，可能通过动态键使用):`);
    unusedKeys.forEach((k) => console.log(`   - ${k}`));
  }

  if (!hasError) {
    console.log('\n✅ i18n 键一致性检查通过！');
    console.log(`   zh-CN: ${zhKeys.size} 个键`);
    console.log(`   en-US: ${enKeys.size} 个键`);
    console.log(`   代码引用: ${usedKeys.size} 个键`);
  }

  process.exit(hasError ? 1 : 0);
}

main();
