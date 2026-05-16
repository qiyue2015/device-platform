/* eslint-disable func-names */
import pluralize from 'pluralize';
import viewGenerator from './plop-templates/view/prompt.mjs';

export default function (plop) {
  // 删除中划线并全部转换为大写
  plop.setHelper('removeDashAndUpperCase', function (text) {
    return text.replace(/-/g, '').toUpperCase();
  });

  // 用于将中划线转换为下划线
  plop.setHelper('upperWithUnderscore', function (text) {
    return text.replace(/-/g, '_');
  });

  // 单词复数转换
  plop.setHelper('pluralize', function (text) {
    return pluralize(text);
  });

  plop.setGenerator('view', viewGenerator);
}
