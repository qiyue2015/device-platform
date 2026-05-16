/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx,less,vue}'],
  darkMode: ['class', '[arco-theme="dark"]'],
  theme: {
    extend: {
      container: {
        center: true,
      },
    },
  },
  plugins: [],
};
