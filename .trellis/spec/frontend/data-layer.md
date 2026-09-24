# 数据层与请求约定（前端）

> 写任何取数、写数、缓存相关的代码前先读这里。**所有 DTO 都靠运行时投影产生，禁止 `as` 断言。**

---

## 三层分工

| 层 | 位置 | 职责 |
|---|---|---|
| 传输层 | `web/src/api/` | `createApiClient(deps)` → `{ request, requestWithResponse }`，信封解包、鉴权头、错误类型 |
| DTO 层 | `web/src/api/types.ts`、`web/src/api/control/types.ts` | 控制面 DTO 与协议常量 |
| 资源层 | `web/src/app/resources/` | 一个后端资源一个文件：运行时校验 + 请求函数 + queryOptions + 缓存写入 |

---

## 传输层（`web/src/api/client.ts`）

- **原生 `fetch`，没有 axios**。
- 路径强约束 `type ApiPath = \`/api/${string}\``，并有 `isSafeApiPath` 做路径注入防护（拒绝 `..`、`\`、控制字符、二次百分号编码）。
- 统一解包 `{ code, message, data }` 信封：`code === 0` 成功，否则抛 `ApiError(status, code, message, data, retryAfterSeconds)`。
- 自动注入 `Authorization: Bearer` 与 `Accept-Language: <locale>`。
- 401 全局只处理一次（`generation` 计数）；`AbortError` 转 `RequestCancelledError`；网络失败转 `NetworkError`。
- **错误类型集中在 `web/src/api/errors.ts`**（`ApiError` / `NetworkError` / `InvalidResponseError` / `RequestCancelledError` / `InvalidRequestPathError`），业务层靠 `instanceof` 分支。
- 后端返回的 `message` **已经是本地化好的文案，直接展示**；`code` 只用于程序化分支。

---

## 资源层四段式（照抄 `web/src/app/resources/channels.ts`）

每个资源文件严格按这四段组织：

1. **字段白名单与枚举字面量**：
   ```ts
   const groupFields = [...] as const
   const groupCollectionItemFields = [...] as const
   ```
2. **`projectXxx(value: unknown): XxxDto` 运行时校验函数**（放最前，纯函数）：
   ```ts
   const record = projectRecord(value)
   assertNoSecretLikeFields(record, groupCollectionItemFields)
   ```
3. **请求函数**，签名固定 —— **第一个参数永远是 `client: ApiClient`，最后一个永远是 `signal?: AbortSignal`**：
   ```ts
   export async function listGroupCollection(
     client: ApiClient, filters: GroupCollectionFilters, signal?: AbortSignal,
   ): Promise<GroupCollectionPage>
   ```
4. **`export function xxxQueryOptions(client, ...)`** 返回 `queryOptions({...})`。

**投影工具在 `web/src/app/resources/projector.ts`**：`projectRecord` / `projectArray` / `projectString` / `projectBoolean` / `projectSafeInteger` / `projectEnum` / `projectHTTPURL` / `projectFiniteNumber` … 失败一律 `throw new InvalidResponseError()`。

两条容易忽略的纪律：

- **绝不使用 `as` 断言**。所有 DTO 都从 `unknown` 手动投影出来。
- **`assertNoSecretLikeFields` 是安全防线，不是风格洁癖**：**白名单外的任何字段**都会 `invalidResponse()`。注意函数里 `secretLikeField.test(field)` 之后紧跟的 `invalidResponse()` 是**无条件**执行的，正则并不构成放行条件 —— 不要以为"名字不像密钥的字段"会被忽略。后端往 classic 读到的响应里加字段 = classic 整页报错，对策见 [backend/directory-structure.md](../backend/directory-structure.md) 的「DTO 与请求体」。
- **跨字段一致性断言也是允许且必要的**（`groups.ts:469`）：
  ```ts
  if (items.length !== total || pending > total ||
      new Set(items.map(({ client_model }) => client_model)).size !== items.length) {
    throw new InvalidResponseError()
  }
  ```

---

## 新增分组级设置键：classic 六处 + modern 一处联动

分组设置页的开关/数值键在多处联动，**漏一处不会报错，只会静默失效或整页报错**。新增一个键必须同时改：

### classic（`web/src/frontends/classic/`）

| # | 位置 | 漏掉的后果 |
|---|---|---|
| 1 | `app/resources/groups.ts` 的 `runtimeSettingFields` | **分组设置页整页 `InvalidResponseError`**（`effective` 走 `complete=true`，用该列表当严格白名单；`groupRuntimeSettingFields` 是它的 spread 派生，加一处即两处生效） |
| 2 | 同文件 `GroupRuntimeConfigDto`（可选 `?`，喂 `overrides`）与 `GroupEffectiveConfigDto`（必填，喂 `effective`） | `vue-tsc --noEmit` 编译报错 |
| 3 | 同文件 `projectRuntimeConfig` 的投影分支 | 字段静默丢失，页面上恒为默认值 |
| 4 | `api/control/types.ts` 的同名两个 DTO（`GroupSettingsDto` 的实际类型来源） | `vue-tsc --noEmit` 编译报错 |
| 5 | `features/groups/settings/group-settings-patch.ts` 的 `cloneOverrides` | **保存时静默丢弃该键**，dirty 比对同时失真 |
| 6 | `features/groups/settings/GroupSettingsTab.vue` 的 computed / toggle+set 函数 / `SettingRow` 模板行 | 键存在但页面上没有可操作的控件 |

`SettingRow` 的 `divided` 默认 `true`（画底部分隔线）：新行若成为该视觉块的**最后一行**，要给它 `:divided="false"`，并去掉原最后一行的 `divided`。

### modern（`web/src/frontends/modern/`）

| # | 位置 | 漏掉的后果 |
|---|---|---|
| 1 | `api/group-detail.ts` 的 `runtimeSwitches` | `readRuntime` 丢弃该键、保存时把它抹掉。渲染（`v-for`）与提交循环都从该列表派生，登记后自动生效，无需改组件 |

### i18n（两套都要）

- classic：`i18n/locales/{zh-CN,en-US,ja-JP}/group.ts` 各加 **label 与 help 两个键**，命名沿用既有惯例（`affinity_enabled` / `affinityHelp`）。
- modern：`i18n/locales/group-detail.ts` 的 `runtimeFields` 三语言块（`enUS: typeof zhCN` 强制键一致），只需 label。

**这一步没有脚本守护，漏了不会有任何报错**，详见 [i18n.md](./i18n.md)。

> **Warning**：后端往共享响应结构体加字段、而前端白名单没登记 = 前端整页报错；后端的投影变更与两套前端的登记**必须放同一个提交**，否则中间那个提交是坏的、不可二分。
>
> 后端侧的键作用域判定（公共 runtime 键 vs 分组专属键）见 [backend/runtime-settings.md](../backend/runtime-settings.md)。

---

## TanStack Vue Query 约定

全局 client（`web/src/app/query.ts`）**只设 `retry: false`**（queries 与 mutations 都是）。重试策略由业务决定，不设默认重试。

**Query key 集中在 `web/src/app/query-keys.ts` 的 `controlQueryKeys`**，统一 `['control', ...]` 开头，每层提供 `all` / `xxxAll()` 前缀常量 + 带参数函数：

```ts
groups: {
  all: ['control', 'groups'] as const,
  collection: (filters: GroupCollectionFilters) =>
    ['control', 'groups', 'collection', normalizeGroupCollectionFilters(filters)] as const,
  summary: (id: number) => ['control', 'groups', 'summary', id] as const,
}
```

**关键约定：凡把 filters 对象塞进 key，必先过同文件的 `normalizeXxxFilters()` 归一化** —— 否则对象引用变化会让缓存键失效。

**默认 `staleTime: Number.POSITIVE_INFINITY` + 手动刷新**（`groups.ts:730`）：

```ts
const manualGroupQueryOptions = {
  staleTime: Number.POSITIVE_INFINITY,
  refetchOnWindowFocus: false,
  refetchOnReconnect: false,
} as const
```

例外（注释里明确写出理由）：目录型数据加 `refetchOnMount: 'always'`；小字典数据用 `staleTime: 5 * 60 * 1_000`。

其他：

- **分页用 `placeholderData: keepPreviousData`**，配合 `useCollectionLoading` 区分 initial / transition / refreshing。
- **queryFn 从 queryKey 反解参数**：`queryFn: ({ queryKey, signal }) => listGroupCollection(client, queryKey[3], signal)` —— 索引取值是既有写法。
- **组件内几乎不用 `useMutation`**。主流是 `useQuery` + 手写 async handler + `pending` ref + 竞态守卫。

---

## 失效：集中式计划表

**不允许在组件里手写 `invalidateQueries`。** 所有 mutation 的失效在 `web/src/app/resources/invalidation.ts` 的 `mutationInvalidationPlans` 登记，按 `settings.update` / `group.create` / `accessKey.update` / `modelPrice.sync` 枚举，每项给出 `plan(exact[], prefixes[])`，由 `applyInvalidationPlan(queryClient, plan)` 顺序执行。

局部精确失效用资源文件里的封装（`groups.ts:900`），**每个 invalidate 都显式写 `exact` 与 `refetchType`**。

**乐观更新用 `setQueryData` 精确写，不改动无关字段**，注释写明契约：

```ts
/** Cache writes are exact and never cause a background refetch. */
export function cacheGroupSettings(queryClient, groupID, settings): void {
  queryClient.setQueryData(controlQueryKeys.groups.settings(groupID), settings)
}
```

**删除时用 `removeQueries`**（非 invalidate），见 `clearGroupResourceCaches`。

---

## 写操作：幂等与未知结果

- POST 建组 / 导凭据**显式传** `headers: { 'Idempotency-Key': idempotencyKey }`。
- 结果用 `web/src/app/mutation-outcome.ts` 的 `classifyMutationOutcome` 归一成 **`confirmed` / `reconciling` / `failed` / `indeterminate`** 四态 —— 这是本项目对"请求已发出但结果未知"的统一定义。它按 HTTP 状态与 `code` 分支（`503` + `CONTROL_OPERATION_INCOMPLETE`、`CONTROL_RECOVERY_PENDING`、`IDEMPOTENCY_RESULT_EXPIRED` 各走不同分支）。

---

## 竞态：请求所有权模式

异步写操作的**标准范本**是 `web/src/features/settings/use-settings-controller.ts`：

```ts
requestOwner += 1
const owner = requestOwner
const controller = new AbortController()
// ...
if (!isCurrent(owner, controller)) return      // 过期响应直接丢弃
```

配套动作：

- 提交前 `await queryClient.cancelQueries({ queryKey: settingsQueryKey(), exact: true })`。
- 成功后 `setQueryData` + `applyInvalidationPlan`。
- `onBeforeUnmount` 里 `mounted = false; requestOwner += 1; requestController?.abort()`。
- `watch(resource, consumeCurrentResource)` **只在"干净"时接受外部新数据** —— 这是本项目处理"后台刷新 vs 用户编辑"冲突的标准答案。

---

## 反模式

- ❌ 用 `as` 断言 DTO —— 必须走 projector。
- ❌ 在组件里手写 `invalidateQueries` —— 去 `invalidation.ts` 登记。
- ❌ 把未归一化的 filters 对象塞进 query key。
- ❌ 在资源层之外构造 API 路径或直接调 `fetch`。
- ❌ 让 `staleTime` 保持默认 —— 本项目默认是 `Infinity` + 手动刷新，改动要写理由。
- ❌ 把后端已本地化的 `message` 再拿去查前端 i18n 表重写。
- ❌ 不做竞态守卫就发写请求。

---

## 范本文件

- `web/src/app/resources/channels.ts` —— 资源层四段式（最短最全）。
- `web/src/app/resources/groups.ts` —— 带 mutation / 缓存写入 / 失效计划的完整版。
- `web/src/features/settings/use-settings-controller.ts` —— 异步写操作状态机。
