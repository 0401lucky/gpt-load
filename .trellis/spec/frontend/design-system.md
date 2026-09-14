# 设计系统与样式（前端）

> 写任何 CSS 或基础组件前先读这里。**硬编码颜色是本项目明确禁止的**（有极少数成对例外，见下）。

---

## 技术栈

**原生 CSS + CSS 自定义属性（设计令牌）+ `<style scoped>`。**

Tailwind **只用于 preflight**：`web/src/styles/base.css:1` 是 `@import 'tailwindcss';`，但全仓库**没有 `tailwind.config.*`、没有 `@theme`/`@utility`/`@apply`、模板里也没有工具类**。不要引入 Tailwind 工具类写法。

（排查提示：代码里搜到的 `@apply` 全是 Vue 事件绑定 `@apply="applyFilters"`。）

---

## 令牌：`web/src/styles/tokens.css`（唯一定义处）

三类：

1. **颜色**（语义命名，每个语义色带 `-bg` 变体）：`--color-canvas` / `-surface` / `-surface-raised` / `-surface-sunken` / `-text` / `-text-muted` / `-text-faint` / `-border-subtle` / `-border-control` / `-action` / `-action-hover` / `-action-soft` / `-focus` / `-overlay` / `-success` / `-info` / `-warning` / `-danger` / `-neutral` / `-skeleton-*`。
2. **排版 / 尺寸 / 间距 / 圆角 / 阴影 / 动效 / 层级**：`--font-{sans,serif,mono}`、`--text-*`、`--title-*`、`--space-*`、`--radius-*`、`--control-*`、`--shadow-*`、`--duration-*`、`--easing-*`、`--z-*`。
3. **派生色用 `color-mix(in srgb, ...)`，不要造新 hex**：
   ```css
   --color-feedback-danger-border: color-mix(
     in srgb, var(--color-danger) 34%, var(--color-border-subtle)
   );
   ```

---

## 明暗主题

`tokens.css` 里三段式，值几乎完全重复：

```css
:root { /* 亮色 */ }
:root[data-theme='dark'] { /* 暗色 */ }
@media (prefers-color-scheme: dark) { :root:not([data-theme]) { /* 暗色 */ } }
```

- `data-theme` 由 `web/src/features/preferences/theme.ts` 写入，`AppTheme = 'system' | 'light' | 'dark'`，`system` 时**移除属性**（交给媒体查询），localStorage key `'gpt-load.theme'`。
- 首屏防闪烁靠内联脚本 `web/public/theme-bootstrap.js`（在 `web/index.html` 里 `<script src="/theme-bootstrap.js">`）。

---

## 颜色纪律

整体是**强禁止**硬编码：800+ 个 Vue 文件里只有 20 处 hex，集中在三类合理场景：

1. **`light-dark(亮, 暗)` 双值**（同一声明里同时给亮暗）：
   ```css
   /* web/src/components/ui/QuotaProgressBar.vue:63 */
   .quota-progress--success { background: light-dark(#dff6e6, #1b3c29); }
   ```
2. **第三方品牌色**（不可主题化）：GitHub 图标、Telegram 渐变。
3. **图表填充色常量**：`QuotaProgressBar.vue:75` 的 `#42be65 / #f1c21b / #fa4d56`。

**规则：禁止新增单值 hex。要么用 `var(--color-*)`，要么用 `light-dark(亮, 暗)` 成对给出。**

---

## 组件与样式约定

**基础组件（`components/ui/*`）只用 props / slots / `v-model` 通信，不注入 API client、不调 `useQuery`。**

**变体用 BEM 修饰类 + 联合类型 prop**，模板拼字符串：

```html
<!-- web/src/components/ui/AppButton.vue:25 -->
:class="[`app-button--${variant}`, `app-button--${size}`, `app-button--tone-${tone}`]"
```

**布局变量通过自定义属性下传**（父组件覆盖子组件 CSS 变量）：

```css
/* web/src/features/groups/GroupsView.vue:639 */
.groups-record-grid { --ledger-record-list-grid: minmax(0, 1fr) 140px ...; }
```

**样式该放哪**：

- 组件自己的 CSS 写在 `.vue` 的 `<style scoped>` 里。
- **只有被多个组件直接引用的类**才提升到 `web/src/styles/components.css`（`.section-nav*`、`.sticky-save-bar*`、`.setting-panel*`）。
- 渲染在 portal 里的基础组件故意用**非 scoped** `<style>`（`AppDialog.vue:86`）。

**类名一律 BEM kebab-case**：`app-dialog__content--ledger`、`quota-progress__fill--warning`。

---

## 无障碍是硬要求

- 交互组件传 `aria-busy` 等状态（`AppButton`）。
- `DataTable.vue:20-37` 用 `ResizeObserver` + `useId()` 拼 `aria-describedby`。
- 模板里保留 `role="columnheader"` / `role="cell"` / `aria-rowindex` 语义标注（见 `GroupsView.vue`）。
- `base.css` 定义 `.sr-only`、统一 `:focus-visible` focus ring，并用 `prefers-reduced-motion` 把 `--duration-*` 压到 `0.01ms`。
- 移动端触控目标统一 `var(--touch-target)`（44px）。

---

## 反模式

- ❌ 新增单值 hex（除非是第三方品牌色）。
- ❌ 引入 Tailwind 工具类、`@apply`、新建 tailwind config。
- ❌ 在页面 / 业务组件里重复定义按钮、卡片、状态、表单、表格的基础样式 —— 先找 `components/ui/` 有没有现成的。
- ❌ 让 `components/ui/*` 里的组件引入业务 DTO 或发请求。
- ❌ 新增全局 CSS 类，除非它真的被多个组件直接引用。
- ❌ 用非 BEM 的类名风格。
- ❌ 用颜色单独表达状态（必须同时有图标、文字、颜色）。

---

## 权威来源

上游文档称正式视觉事实源是 Notion《GPT-Load 2.0 M3 前端视觉规范》。**改动视觉方向、token 或全局组件规则前先与用户确认**，不要自作主张重排设计语言。
