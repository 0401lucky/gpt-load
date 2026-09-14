# 目录结构与组件分层（前端）

> 决定"新文件该放哪"时先读这里。判断依据是**代码是否知道业务实体**，不是文件类型。

---

## web/src 顶层目录职责

| 目录 | 职责 | 范例 |
|---|---|---|
| `web/src/app/` | 应用外壳与横切基础设施：路由、query client、query key、toast、DI 控制器、加载状态、通用 composable | `app/router.ts`、`app/query-keys.ts`、`app/toast.ts` |
| `web/src/app/resources/` | **数据资源层**：一个后端资源一个文件，负责运行时校验 + queryOptions + mutation + 缓存写入 | `app/resources/groups.ts` |
| `web/src/app/*.vue` | 应用外壳组件（**只有 3 个**） | `AppShell.vue`、`AuthGate.vue`、`RouteAnnouncer.vue` |
| `web/src/api/` | HTTP 传输层：fetch 封装、错误类型、信封解析、DTO | `api/client.ts`、`api/errors.ts`、`api/types.ts` |
| `web/src/api/control/` | 控制面 DTO 与协议常量 | `api/control/types.ts`、`api/control/protocols.ts` |
| `web/src/components/` | **跨 feature 复用**的组件，按通用度分 6 个子目录 | `ui/`、`layout/`、`collection/`、`config/`、`charts/`、`brand/` |
| `web/src/features/` | **业务 feature**，12 个，每个自包含 View + 子组件 + 逻辑模块 | `features/groups/`、`features/monitor/` |
| `web/src/composables/` | 全局可复用 composable（目前仅 1 个） | `composables/use-section-navigation.ts` |
| `web/src/lib/` | **纯函数**工具：无 Vue 依赖、无网络 | `lib/format.ts`、`lib/time.ts` |
| `web/src/i18n/` | vue-i18n 装配 + 按 namespace 懒加载 | `i18n/index.ts`、`i18n/locales/{zh-CN,en-US,ja-JP}/` |
| `web/src/styles/` | 3 个全局 CSS：令牌 / reset / 跨组件共享类 | `styles/tokens.css`、`base.css`、`components.css` |

> `web/src/views/` 下只有一个 `PlaceholderView.vue`，全仓库无任何引用（历史遗留）。新增页面**不要**往这里放。

---

## 组件该放哪

判据：**这个组件知道业务实体吗？**

- `components/ui/` —— 完全无业务语义，纯 props 驱动，**不注入 API client、不调 `useQuery`**（`AppButton.vue` 的 script 只有 `withDefaults(defineProps<...>)`，零逻辑）。命名规则：
  - `App*` 前缀 = 基础控件（`AppButton` / `AppSelect` / `AppTabs` / `AppToastViewport`）；少数用 `defineOptions({ name: 'AppSurface' })` 保留前缀（`components/ui/Surface.vue:2`）。
  - 无前缀 = 领域无关的通用块（`DataTable` / `EmptyState` / `StatusBadge` / `SkeletonSurface` / `PaginationBar`）。
- `components/{collection,config,layout,charts,brand}/` —— 跨 feature 复用的**领域半通用**组件（集合页骨架、配置编辑器、页面布局）。
- `features/<x>/` —— 只被该 feature 使用，或直接绑定该 feature 的 DTO。feature 内部按子域再分层，如 `features/groups/{credentials,models,settings}/`。

已有注释明确这条规则，`components/brand/ChannelIcon.vue:11-13`：把 ChannelID 映射到资产属于 channel 定义/编译器，**绝不属于单个 view**。

---

## 命名约定

- **组件文件：`PascalCase.vue`**，无一例外。feature 组件用 feature 名做前缀（`GroupSettingsTab.vue`、`AccessKeyDrawer.vue`）。
- **非组件 TS 模块：`kebab-case.ts`，且后缀带语义**（这是本项目最容易被忽略的一条）：

| 后缀 | 含义 | 范例 |
|---|---|---|
| `*-route.ts` | 路由 query 的解析/序列化 | `features/groups/group-collection-route.ts` |
| `*-patch.ts` | 表单草稿 → 局部更新体的 diff | `features/settings/settings-patch.ts` |
| `*-presenter.ts` | 纯展示派生 | `features/home/home-presenter.ts` |
| `*-operation.ts` | 长事务 / 幂等操作编排 | `features/import/import-operation.ts` |
| `use-*.ts` | composable | `features/settings/use-settings-controller.ts` |

- **没有 barrel / index 文件**（全 `src` 只有 `i18n/index.ts`）。导入一律写全路径：`import AppButton from '@/components/ui/AppButton.vue'`。
- 路径别名只有 `@/*` → `./src/*`（`web/tsconfig.app.json` + `web/vite.config.ts`）。**feature 内部用相对导入**（`./group-route`、`../group-route`）。

---

## 导入顺序

`features/groups/GroupsView.vue` 是标准顺序：

```
lucide → @tanstack → vue / vue-i18n / vue-router → @/api/* → @/app/* → @/components/* → 相对路径
```

---

## 反模式

- ❌ 往 `components/ui/` 放任何知道业务 DTO 的组件。
- ❌ 新建 barrel `index.ts` 聚合导出。
- ❌ 把一次性页面组件放进 `components/`（它属于 `features/`）。
- ❌ 在 `lib/` 里引入 Vue 或发请求 —— 那里必须是纯函数。
- ❌ 用 `views/` 目录。

---

## 范本文件

- `web/src/app/resources/channels.ts` —— 资源层四段式骨架（最短的一个，一屏看完）。
- `web/src/features/settings/use-settings-controller.ts` —— 复杂表单页状态机。
- `web/src/features/groups/GroupsView.vue` —— feature 页面装配全貌。
