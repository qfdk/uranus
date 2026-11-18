// 颜色用语义名称，具体数值在 input.css 里按浅色/深色主题定义
const token = (name) => `rgb(var(--${name}) / <alpha-value>)`;

module.exports = {
  content: ["./web/**/*.html", "./web/public/js/**/*.js"],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        bg: token("bg"),
        surface: token("surface"),
        card: token("card"),
        line: token("line"),
        "line-soft": token("line-soft"),
        fg: token("fg"),
        muted: token("muted"),
        subtle: token("subtle"),
        hover: token("hover"),
        primary: { DEFAULT: token("primary"), fg: token("primary-fg") },
        accent: token("accent"),
      },
      fontFamily: {
        sans: [
          "-apple-system", "BlinkMacSystemFont", '"Segoe UI"', "Roboto",
          '"PingFang SC"', '"Hiragino Sans GB"', '"Microsoft YaHei"', '"Noto Sans CJK SC"',
          '"Helvetica Neue"', "Arial", "sans-serif",
        ],
      },
    },
  },
  plugins: [],
}
