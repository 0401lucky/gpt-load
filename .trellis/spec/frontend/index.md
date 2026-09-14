# 前端开发准则（Frontend Guidelines）

> `web/` 下 Vue 3 管理前端的实际工程约定。**写代码前先读这里**，再按下面的清单进入对应文件。

---

## Pre-Development Checklist

| 你要做的事 | 必读 |
|---|---|
| 决定新组件/新模块放哪、命名 | [directory-structure.md](./directory-structure.md) |
| 写 CSS、加组件变体、改主题 | [design-system.md](./design-system.md) |
| 取数、写数、加 query key、改缓存 | [data-layer.md](./data-layer.md) |
| 加用户可见文案 | [i18n.md](./i18n.md) |
| 提交前自查、写组件/composable | [quality-guidelines.md](./quality-guidelines.md) |

**跨层改动**（前端改了字段、后端要跟着改）额外读 `.trellis/spec/guides/cross-layer-thinking-guide.md`。

**改动前的固定动作**：`grep` 你要改的那个常量/字段/token。前端很多值是多处联动的（query key、失效计划、locale key、design token）。

---

## 四条最容易踩的红线

1. **后端数据必须走 projector 投影，禁止 `as` 断言**。`app/resources/projector.ts` 的 `assertNoSecretLikeFields` 是防凭据泄漏的安全防线，不是风格洁癖。见 [data-layer.md](./data-layer.md)。
2. **缓存失效只能去 `invalidation.ts` 登记**，不许在组件里手写 `invalidateQueries`。见 [data-layer.md](./data-layer.md)。
3. **禁止新增单值 hex**。要么 `var(--color-*)`，要么 `light-dark(亮, 暗)` 成对给出。见 [design-system.md](./design-system.md)。
4. **新增文案必须三套 locale 同步**（`zh-CN` / `en-US` / `ja-JP`）—— 这一步**没有脚本守护**，漏了不会有任何报错。见 [i18n.md](./i18n.md)。

---

## Quality Check

声称完成之前逐条核对：

- [ ] `make check` 通过（`lint --max-warnings=0`、`prettier --check`、`vue-tsc --noEmit` 都干净）。
- [ ] 新组件放对了目录（`components/ui/` 必须无业务语义、不发请求）。
- [ ] 取数走资源层，query key 来自 `query-keys.ts` 且 filters 过了 `normalizeXxxFilters`。
- [ ] mutation 的失效已在 `invalidation.ts` 登记。
- [ ] 写操作有竞态守卫（`requestOwner` + `AbortController` + `isCurrent`）。
- [ ] 新增文案三套 locale 都加了，key 层级符合既有组织。
- [ ] 没有新增单值 hex；变体用 BEM 修饰类 + 联合类型 prop。
- [ ] 类型导入用 `import type`。
- [ ] 交互元素有无障碍标注（`role` / `aria-*` / 触控目标 44px）。
- [ ] 没有引入前端测试框架（本项目明确不写前端测试）。

---

## 技术栈快照

| 项 | 值 |
|---|---|
| 框架 | Vue 3（100% `<script setup lang="ts">`） |
| 构建 | Vite，产物输出到 `../internal/webui/dist`（内嵌进 Go 二进制） |
| 数据 | TanStack Vue Query（**默认 `retry: false`、`staleTime: Infinity`**） |
| i18n | vue-i18n，3 语言 × 8 namespace 懒加载 |
| 样式 | 原生 CSS + design token（Tailwind 仅 preflight） |
| 状态 | 无 pinia —— query cache + provide/inject + URL |
| 测试 | **无**（靠静态门禁 + Go 侧契约测试） |
| 包管理 | pnpm（corepack），依赖精确 pin |

---

**语言**：本 spec 用简体中文书写（与项目注释惯例一致，标识符与代码保持英文）。
