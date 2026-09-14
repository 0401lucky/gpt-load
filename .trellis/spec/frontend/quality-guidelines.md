# 质量与写法准则（前端）

> 写 Vue 组件、composable、提交前自查时读这里。

---

## 门禁

`web/package.json` 只有 6 个 script：

```json
"dev": "vite",
"build": "pnpm run type-check && vite build",
"lint": "eslint . --max-warnings=0",
"format": "prettier --check .",
"format:write": "prettier --write .",
"type-check": "vue-tsc --noEmit -p tsconfig.app.json && tsc --noEmit -p tsconfig.node.json"
```

要点：

- **`build` 不是单纯打包**，它串了 `type-check`。
- **`lint` 带 `--max-warnings=0`** —— 一个 warning 就失败。
- `format` 只检查不改写，写用 `format:write`。
- 依赖版本**全部精确 pin，无 `^`/`~`**；`engines` 要求 node >=24.11.0，`packageManager` 锁 `pnpm@11.17.0`。

完整门禁仍然是仓库根的 `make check`（会自动跑上面的 lint / format / build）。

---

## 前端不做测试

`web/` 下**没有** vitest / jest / playwright / cypress，没有 `test` script，没有任何 `*.test.ts` / `*.spec.ts` / `__tests__` / e2e 目录，`vite.config.ts` 也没有 `test` 字段。

质量保障靠：**静态门禁 + Go 侧契约测试**。前后端契约由 `internal/webui/*_test.go` 校验 `internal/webui/page_routes.json`，前端 `web/src/app/page-routes.ts` 在模块加载时解析该 JSON 并 `Object.freeze`，非法即抛错。

**不要给前端补测试框架**，除非用户明确要求。

---

## TypeScript 配置

`web/tsconfig.app.json`：`strict: true`、**`verbatimModuleSyntax: true`**（所以类型导入必须写 `import type { ... }`，全仓库一致）、`moduleResolution: Bundler`、`isolatedModules`。

**没有** `noUncheckedIndexedAccess`、**没有** `exactOptionalPropertyTypes`。

`tsconfig.json` 只做 project references（app + node）；`tsconfig.node.json` 单独校验 `vite.config.ts`。

---

## ESLint / Prettier

`web/eslint.config.mjs` 极简（11 行）：`eslint-plugin-vue` flat/recommended + TS recommended + `eslint-config-prettier`。**没有自定义规则、没有 import 排序插件、不用 type-aware linting。**

全仓库**只有 1 处 `eslint-disable`**，且带理由注释（`components/brand/ChannelIcon.vue:24`）—— 这形成一条事实约定：**禁用规则必须写原因**。

Prettier（`web/.prettierrc.json`）：`semi: false`、`singleQuote: true`、`trailingComma: 'all'`、`printWidth: 100`。

---

## 组件写法

- **100% `<script setup lang="ts">`**。文件内顺序固定：`<script setup>` → `<template>` → `<style scoped>`。
- **Props 用 TS 泛型 + `withDefaults`，不用运行时声明**，props 名 camelCase：
  ```ts
  const props = withDefaults(
    defineProps<{
      items: readonly SectionNavItem[]
      modelValue: string
      appearance?: 'default' | 'ledger'
    }>(),
    { caption: undefined, appearance: 'default' },
  )
  ```
  只读数组写 `readonly X[]`；可选 props 显式补 `undefined` 默认值。
- **`defineEmits` 用元组式类型签名**：`defineEmits<{ 'update:param': [key: string, value: string | null] }>()`。
- **`defineModel` 仅在确有双向绑定需求时使用**（全仓 4 处）。默认仍是 `modelValue` prop + `update:modelValue` emit。
- **`defineExpose` 用于命令式 API**（22 处），如 `defineExpose({ openFilters, refresh, filterCount })`。
- **输入类组件用 `defineOptions({ inheritAttrs: false })`** 显式转发 attrs（`AppTextInput`、`AppSelect`、`AppSearchInput` 等）。
- **模板里 props 一律 kebab-case**：`:model-value`、`:row-count`。
- **注释写"为什么"而非"做什么"**，中文，常带决策依据：
  ```ts
  // weight_manual 为 0 也判定 disabled，但接口限定 1~100，故 disabled 即已停用。
  // AppSwitch 纯受控，等请求走完才翻转会像卡住，故先本地置位。
  ```

---

## Composable 与依赖注入

- **控制器三件套模式**（`web/src/app/toast.ts:60`）：
  ```ts
  export const toastKey: InjectionKey<ToastController> = Symbol('toast')

  export function useToast(): ToastController {
    const controller = inject(toastKey)
    if (!controller) throw new Error('TOAST_NOT_PROVIDED')
    return controller
  }
  ```
  七个跨层服务都在 `web/src/main.ts:104-110` 提供：`authSession` / `importRecovery` / `unsavedChanges` / `toast` / `apiClient` / `appI18n` / `themeController`。
- **工厂接收 deps 以便测试**：`createToastController(deps)` 显式接收 `{ fetch, storage, setTimer, clearTimer, matchMedia, now }`；`createBrowserThemeController(browser, documentElement, storage?)`。
- **composable 参数用 `MaybeRefOrGetter<T>`** 而非 `Ref<T>`，内部 `toValue()` 解包。
- **定时器 / 监听 / AbortController 在 `onScopeDispose` / `onBeforeUnmount` 清理**。
- 返回类型显式声明为接口（`SettingsPageController`、`ToastController`），暴露只读 ref + 动作方法。

---

## 状态管理

**没有 pinia / Vuex / 模块级单例 store。** 状态分三层：

1. **服务端状态 → TanStack Query 缓存**（唯一真源），跨组件共享靠 query key 相同，而非共享 ref。
2. **跨层应用服务 → `createXxx()` 工厂 + provide/inject**（见上）。
3. **页面局部状态 → 组件内 `ref`/`computed`，URL 作为可分享状态的载体** —— 筛选、分页、当前 tab、抽屉展开项全部编码进 query string，配套 `*-route.ts` 的 `parse*/serialize*` 函数，并有 `isCanonicalRouteQuery` 校验 + 非规范时 `router.replace` 自愈。

`web/src/app/ephemeral-state.ts` 的 `clearEphemeralState()` 在登出/401 时集中清理瞬时状态。

---

## 导入顺序

`features/groups/GroupsView.vue` 是标准：

```
lucide → @tanstack → vue / vue-i18n / vue-router → @/api/* → @/app/* → @/components/* → 相对路径
```

---

## 反模式

- ❌ 引入 pinia 或新建全局 store。
- ❌ 给前端补测试框架（除非用户明确要求）。
- ❌ 在 `components/ui/*` 里发请求或引入业务 DTO。
- ❌ 用 `as` 断言后端数据（走 projector 投影）。
- ❌ 加 `eslint-disable` 不写理由。
- ❌ 用运行时 props 声明（`defineProps({...})` 字面量形式）。
- ❌ 双向绑定滥用 `defineModel`（默认用 `modelValue` + emit）。
- ❌ 在组件卸载时不清理定时器/监听/AbortController。

---

## Code Review 自查清单

- [ ] `make check` 通过（含 lint 零 warning、prettier check、type-check）。
- [ ] 新增文案三套 locale 同步（含 `en-US`）。
- [ ] 新组件放对了目录（判据见 `directory-structure.md`）。
- [ ] 没有新增单值 hex（见 `design-system.md`）。
- [ ] 数据访问走资源层 + `query-keys.ts`，失效在 `invalidation.ts` 登记。
- [ ] 写操作有竞态守卫（`requestOwner` + AbortController）。
- [ ] 类型导入用了 `import type`。
- [ ] 交互元素有无障碍标注（role / aria-*）。
