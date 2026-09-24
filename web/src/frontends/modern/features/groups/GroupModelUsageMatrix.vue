<script setup lang="ts">
import { ChevronDown, ChevronRight, Info, RefreshCw } from '@lucide/vue'
import { useQuery } from '@tanstack/vue-query'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getGroupCredentials, type CredentialFilters } from '@modern/api/group-detail'
import {
  groupRowsByModel,
  sortModelUsage,
  type GroupModelUsage,
  type GroupModelUsageRow,
  type ModelUsageDirection,
  type ModelUsageSort,
} from '@modern/api/group-model-usage'
import {
  AppBadge,
  AppButton,
  AppCollectionState,
  AppIcon,
  AppIconButton,
  AppOverflowText,
  AppSwitch,
  AppTooltip,
} from '@modern/components/ui'
import { formatCompactNumber, formatRemainingDuration } from '@modern/components/ui/format'
import { useApiClient } from '@shared/http/client-context'
import { credentialTime } from './credential-presentation'

const props = defineProps<{
  groupId: number
  data?: GroupModelUsage
  loading: boolean
  failed: boolean
}>()
const emit = defineEmits<{ retry: [] }>()
const { t, n, locale } = useI18n()
const client = useApiClient()
const onlyCooling = ref(false)
const grouped = ref(false)
const expanded = ref(true)
const sort = ref<ModelUsageSort>('total_tokens')
const direction = ref<ModelUsageDirection>('desc')
const collapsed = ref<Set<string>>(new Set())

// 矩阵只下发 credential_id；可识别的标识来自该组凭据列表。后端只接受 page_size ≤ 100，
// 凭据可能有一页以上，因此按页取全后合并；任一分页失败即停止翻页，用已取到的部分建映射，
// 未覆盖的组合回退到内部 id，矩阵本身不受影响。
const credentialIdentityPageSize = 100
// 翻页硬上限：最多 10 页（1000 个凭据），避免凭据异常多时打出无限请求。
const credentialIdentityPageLimit = 10
const identityFilters: CredentialFilters = {
  q: '',
  page: 1,
  pageSize: credentialIdentityPageSize,
  sort: 'name',
  status: '',
  proxy: '',
  reset: '',
}
async function loadCredentialIdentities(signal: AbortSignal) {
  const map = new Map<number, { mask: string; note: string }>()
  for (let page = 1; page <= credentialIdentityPageLimit; page += 1) {
    try {
      const result = await getGroupCredentials(
        client,
        props.groupId,
        { ...identityFilters, page },
        signal,
      )
      for (const item of result.items) map.set(item.id, { mask: item.mask, note: item.note })
      if (page * result.pageSize >= result.total) break
    } catch {
      // 任一页失败就保留已取到的部分，不向上抛错。
      break
    }
  }
  return map
}
// 标识映射单独放一个查询键：既有 groupCredentialsKey 前缀下缓存的是凭据分页响应，
// 其他组件会按该形状 setQueriesData，放 Map 会让那些更新器读到 undefined。
const credentials = useQuery(
  computed(() => ({
    queryKey: ['modern', 'group-credential-identities', props.groupId] as const,
    queryFn: ({ signal }: { signal: AbortSignal }) => loadCredentialIdentities(signal),
    enabled: props.groupId > 0,
  })),
)
const identities = computed(
  () => credentials.data.value ?? new Map<number, { mask: string; note: string }>(),
)

const visible = computed(() =>
  (props.data?.items ?? []).filter((row) => !onlyCooling.value || row.cooldownUntil !== null),
)
const rows = computed(() =>
  sortModelUsage(visible.value, sort.value, direction.value, credentialLabel),
)
const groups = computed(() => groupRowsByModel(rows.value))
function toggleSort(next: ModelUsageSort): void {
  if (sort.value === next) {
    direction.value = direction.value === 'desc' ? 'asc' : 'desc'
    return
  }
  sort.value = next
  // 数值列默认从高到低，标识列默认升序。
  direction.value = next === 'credential_id' || next === 'model' ? 'asc' : 'desc'
}
function ariaSort(column: ModelUsageSort): 'ascending' | 'descending' | 'none' {
  if (sort.value !== column) return 'none'
  return direction.value === 'asc' ? 'ascending' : 'descending'
}
function toggleGroup(model: string): void {
  const next = new Set(collapsed.value)
  if (next.has(model)) next.delete(model)
  else next.add(model)
  collapsed.value = next
}
function rowKey(row: GroupModelUsageRow): string {
  return `${row.credentialID}:${row.model}`
}
function tokensDetail(row: GroupModelUsageRow): string {
  const cacheWrite = row.cacheWrite5mTokens + row.cacheWrite1hTokens + row.cacheWriteUnknownTokens
  return [
    t('groupDetail.modelUsage.tokensDetail.label', {
      input: n(row.uncachedInputTokens + row.cacheReadTokens + cacheWrite),
      output: n(row.outputTokens),
    }),
    t('groupDetail.modelUsage.tokensDetail.cache', {
      read: n(row.cacheReadTokens),
      write: n(cacheWrite),
    }),
    t('groupDetail.modelUsage.windowStart', {
      time: credentialTime(row.windowStart, locale.value),
    }),
  ].join('\n')
}
function requestsDetail(row: GroupModelUsageRow): string {
  return t('groupDetail.modelUsage.requestsDetail', {
    success: n(row.successCount),
    failure: n(row.failureCount),
  })
}
function cooldownText(row: GroupModelUsageRow): string {
  return row.cooldownUntil === null
    ? t('groupDetail.modelUsage.cooldown.none')
    : t('groupDetail.modelUsage.cooldown.until', {
        time: credentialTime(row.cooldownUntil, locale.value),
      })
}
function remaining(row: GroupModelUsageRow): string {
  return row.cooldownUntil === null
    ? ''
    : formatRemainingDuration(row.cooldownUntil - (props.data?.observedAt ?? 0), locale.value)
}
function credentialIDLabel(row: GroupModelUsageRow): string {
  return t('groupDetail.modelUsage.credentialLabel', { id: n(row.credentialID) })
}
// 排序与实际显示共用同一标识：备注优先，其次掩码；两者都缺失时回退到内部 id。
function credentialLabel(credentialID: number): string | undefined {
  const entry = identities.value.get(credentialID)
  return entry?.note || entry?.mask || undefined
}
function credentialIdentity(row: GroupModelUsageRow): string {
  return credentialLabel(row.credentialID) ?? credentialIDLabel(row)
}
function credentialHint(row: GroupModelUsageRow): string {
  const identity = credentialIdentity(row)
  const idLabel = credentialIDLabel(row)
  return identity === idLabel ? idLabel : `${identity}\n${idLabel}`
}
</script>

<template>
  <section class="modern-model-usage" :aria-label="t('groupDetail.modelUsage.title')">
    <header class="modern-model-usage-heading">
      <div class="modern-model-usage-heading-title">
        <AppButton
          variant="text"
          size="xs"
          :icon="expanded ? ChevronDown : ChevronRight"
          :aria-expanded="expanded"
          @click="expanded = !expanded"
          ><span>{{ t('groupDetail.modelUsage.title') }}</span></AppButton
        >
        <p class="modern-model-usage-summary">
          {{ t('groupDetail.modelUsage.description') }}
          <AppTooltip
            v-if="data"
            :label="
              t('groupDetail.modelUsage.hint', {
                from: credentialTime(data.countedFrom, locale),
                to: credentialTime(data.countedTo, locale),
              })
            "
            ><span class="modern-model-usage-info" tabindex="0"
              ><AppIcon :icon="Info" size="xs" /></span
          ></AppTooltip>
        </p>
      </div>
      <div class="modern-model-usage-heading-actions">
        <AppBadge v-if="data">{{ n(visible.length) }}</AppBadge>
        <AppIconButton
          :icon="RefreshCw"
          :label="t('groupDetail.modelUsage.refresh')"
          size="xs"
          :loading="loading"
          @click="emit('retry')"
        />
      </div>
    </header>
    <template v-if="expanded">
      <p v-if="failed && data" class="modern-model-usage-notice">
        {{ t('groupDetail.modelUsage.stale') }}
      </p>
      <p v-if="data?.truncated" class="modern-model-usage-notice">
        {{
          t('groupDetail.modelUsage.truncated', {
            shown: n(data.items.length),
            total: n(data.totalItems),
          })
        }}
      </p>
      <div class="modern-model-usage-toolbar">
        <AppSwitch
          v-model="onlyCooling"
          size="sm"
          :label="t('groupDetail.modelUsage.filters.onlyCooling')"
        />
        <span>{{ t('groupDetail.modelUsage.filters.onlyCooling') }}</span>
        <AppSwitch
          v-model="grouped"
          size="sm"
          :label="t('groupDetail.modelUsage.filters.groupByModel')"
        />
        <span>{{ t('groupDetail.modelUsage.filters.groupByModel') }}</span>
      </div>
      <AppCollectionState
        v-if="failed && !data"
        :title="t('groupDetail.modelUsage.loadFailed')"
        error
        ><AppButton @click="emit('retry')">{{ t('groupDetail.modelUsage.retry') }}</AppButton>
      </AppCollectionState>
      <AppCollectionState v-else-if="!data" :title="t('groupDetail.modelUsage.loading')" loading />
      <p v-else-if="!rows.length" class="modern-model-usage-empty">
        {{
          onlyCooling
            ? t('groupDetail.modelUsage.emptyFiltered')
            : t('groupDetail.modelUsage.empty')
        }}
      </p>
      <div v-else class="modern-model-usage-scroll">
        <table class="modern-model-usage-table">
          <caption class="modern-sr-only">
            {{
              t('groupDetail.modelUsage.title')
            }}
          </caption>
          <thead>
            <tr>
              <th scope="col" :aria-sort="ariaSort('credential_id')">
                <AppButton variant="text" size="xs" @click="toggleSort('credential_id')">{{
                  t('groupDetail.modelUsage.columns.credential')
                }}</AppButton>
              </th>
              <th scope="col" :aria-sort="ariaSort('model')">
                <AppButton variant="text" size="xs" @click="toggleSort('model')">{{
                  t('groupDetail.modelUsage.columns.model')
                }}</AppButton>
              </th>
              <th
                scope="col"
                class="modern-model-usage-number"
                :aria-sort="ariaSort('total_tokens')"
              >
                <AppButton variant="text" size="xs" @click="toggleSort('total_tokens')">{{
                  t('groupDetail.modelUsage.columns.tokens')
                }}</AppButton>
              </th>
              <th
                scope="col"
                class="modern-model-usage-number"
                :aria-sort="ariaSort('request_count')"
              >
                <AppButton variant="text" size="xs" @click="toggleSort('request_count')">{{
                  t('groupDetail.modelUsage.columns.requests')
                }}</AppButton>
              </th>
              <th scope="col" class="modern-model-usage-window">
                <span>{{ t('groupDetail.modelUsage.columns.window') }}</span>
                <AppTooltip :label="t('groupDetail.modelUsage.window.help')"
                  ><span class="modern-model-usage-info" tabindex="0"
                    ><AppIcon :icon="Info" size="xs" /></span
                ></AppTooltip>
              </th>
              <th scope="col">{{ t('groupDetail.modelUsage.columns.cooldown') }}</th>
            </tr>
          </thead>
          <tbody v-for="group in grouped ? groups : [{ model: '', rows }]" :key="group.model">
            <tr v-if="grouped" class="modern-model-usage-group">
              <th colspan="6" scope="colgroup">
                <AppButton
                  variant="text"
                  size="xs"
                  :icon="collapsed.has(group.model) ? ChevronRight : ChevronDown"
                  :aria-expanded="!collapsed.has(group.model)"
                  @click="toggleGroup(group.model)"
                  ><span>{{ group.model }}</span></AppButton
                >
                <small>{{ n(group.rows.length) }}</small>
              </th>
            </tr>
            <template v-if="!grouped || !collapsed.has(group.model)">
              <tr v-for="row in group.rows" :key="rowKey(row)">
                <td class="modern-model-usage-credential">
                  <AppTooltip :label="credentialHint(row)"
                    ><span class="modern-model-usage-credential-text" tabindex="0">{{
                      credentialIdentity(row)
                    }}</span></AppTooltip
                  >
                </td>
                <td>
                  <span class="modern-model-usage-model"
                    ><AppOverflowText :text="row.model"
                  /></span>
                </td>
                <td class="modern-model-usage-number">
                  <AppTooltip :label="tokensDetail(row)"
                    ><span tabindex="0">{{
                      formatCompactNumber(row.totalTokens, locale)
                    }}</span></AppTooltip
                  >
                </td>
                <td class="modern-model-usage-number">
                  <AppTooltip :label="requestsDetail(row)"
                    ><span tabindex="0">{{
                      formatCompactNumber(row.requestCount, locale)
                    }}</span></AppTooltip
                  >
                </td>
                <td>
                  <AppBadge
                    size="xs"
                    :variant="row.windowSource === 'reset' ? 'soft' : 'outline'"
                    :tone="row.windowSource === 'reset' ? 'info' : 'neutral'"
                    >{{
                      row.windowSource === 'reset'
                        ? t('groupDetail.modelUsage.window.exact')
                        : t('groupDetail.modelUsage.window.fallback')
                    }}</AppBadge
                  >
                </td>
                <td class="modern-model-usage-cooldown">
                  <AppBadge v-if="row.cooldownUntil !== null" size="xs" tone="warning" dot>{{
                    t('groupDetail.modelUsage.cooldown.cooling')
                  }}</AppBadge>
                  <AppTooltip :label="cooldownText(row)"
                    ><span class="modern-model-usage-info" tabindex="0"
                      ><AppIcon :icon="Info" size="xs" /></span
                  ></AppTooltip>
                  <small v-if="row.cooldownUntil !== null">{{ remaining(row) }}</small>
                  <span v-else>{{ t('groupDetail.modelUsage.cooldown.none') }}</span>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
    </template>
  </section>
</template>

<style scoped>
.modern-model-usage {
  display: grid;
  flex: none;
  align-content: start;
  gap: var(--modern-space-2);
  min-width: 0;
}
.modern-model-usage-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--modern-space-3);
}
.modern-model-usage-heading-title {
  min-width: 0;
}
.modern-model-usage-summary {
  display: flex;
  align-items: center;
  gap: var(--modern-space-1);
  padding-left: var(--modern-space-1);
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
}
.modern-model-usage-heading-actions {
  display: flex;
  align-items: center;
  gap: var(--modern-space-2);
}
.modern-model-usage-info {
  display: inline-flex;
  vertical-align: middle;
  color: var(--modern-muted);
}
.modern-model-usage-notice,
.modern-model-usage-empty {
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
}
.modern-model-usage-toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--modern-space-1);
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
}
.modern-model-usage-toolbar > span {
  margin-right: var(--modern-space-3);
}
.modern-model-usage-scroll {
  min-width: 0;
  max-height: min(40vh, 340px);
  overflow: auto;
  border: var(--modern-line-width) solid var(--modern-border);
  border-radius: var(--modern-radius-control);
  background: var(--modern-surface);
}
.modern-model-usage-table {
  width: 100%;
  table-layout: fixed;
  border-collapse: collapse;
  text-align: left;
  font-size: var(--modern-font-size-small);
  font-variant-numeric: tabular-nums;
}
.modern-model-usage-table thead th {
  position: sticky;
  top: 0;
  z-index: var(--modern-layer-raised);
  padding: var(--modern-space-2);
  background: var(--modern-surface);
  color: var(--modern-muted);
  font-size: var(--modern-font-size-caption);
  font-weight: var(--modern-weight-regular);
  white-space: nowrap;
}
.modern-model-usage-table td {
  min-width: 0;
  padding: var(--modern-space-2);
  border-top: var(--modern-line-width) solid var(--modern-border);
  color: var(--modern-muted);
  vertical-align: middle;
}
.modern-model-usage-table tbody tr:hover {
  background: var(--modern-control-hover);
}
.modern-model-usage-table th:first-child,
.modern-model-usage-table td:first-child {
  width: 120px;
}
.modern-model-usage-table th:nth-child(3),
.modern-model-usage-table td:nth-child(3) {
  width: 116px;
}
.modern-model-usage-table th:nth-child(4),
.modern-model-usage-table td:nth-child(4) {
  width: 88px;
}
.modern-model-usage-table th:nth-child(5),
.modern-model-usage-table td:nth-child(5) {
  width: 92px;
}
.modern-model-usage-table th:last-child,
.modern-model-usage-table td:last-child {
  width: 148px;
}
.modern-model-usage-number {
  text-align: right;
}
.modern-model-usage-table thead th.modern-model-usage-number .modern-button {
  width: 100%;
  justify-content: flex-end;
}
.modern-model-usage-number,
.modern-model-usage-credential {
  color: var(--modern-text);
}
.modern-model-usage-credential-text {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.modern-model-usage-window {
  white-space: nowrap;
}
.modern-model-usage-model {
  display: block;
  min-width: 0;
  color: var(--modern-text);
  font-family: var(--modern-font-mono);
}
.modern-model-usage-group th {
  display: flex;
  align-items: center;
  gap: var(--modern-space-2);
  padding: var(--modern-space-1) var(--modern-space-2);
  border-top: var(--modern-line-width) solid var(--modern-border);
  background: var(--modern-subtle);
}
.modern-model-usage-group th small {
  margin-left: auto;
  color: var(--modern-muted);
  font-size: var(--modern-font-size-small);
  font-weight: var(--modern-weight-regular);
}
.modern-model-usage-group .modern-button {
  font-family: var(--modern-font-mono);
}
.modern-model-usage-cooldown {
  display: flex;
  align-items: center;
  gap: var(--modern-space-1);
}
.modern-model-usage-cooldown small {
  font-variant-numeric: tabular-nums;
}
</style>
