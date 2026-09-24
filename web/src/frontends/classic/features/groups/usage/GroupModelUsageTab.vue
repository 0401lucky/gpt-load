<script setup lang="ts">
import { useQuery } from '@tanstack/vue-query'
import { ChevronDown, ChevronRight, RefreshCw } from '@lucide/vue'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { useApiClient } from '@shared/http/client-context'
import type { GroupModelUsageItemDto } from '@/api/control/types'
import { useStableLoading } from '@/app/loading-state'
import { controlQueryKeys } from '@/app/query-keys'
import { groupModelUsageQueryOptions } from '@/app/resources/group-model-usage'
import { getCredentialCollection } from '@/app/resources/credentials'
import {
  formatInteger,
  formatISOInstant,
  formatLocalTime,
  formatRelativeInstant,
  formatTokens,
} from '@/lib/format'
import AppButton from '@/components/ui/AppButton.vue'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import AppTooltip from '@/components/ui/AppTooltip.vue'
import AsyncRefreshIndicator from '@/components/ui/AsyncRefreshIndicator.vue'
import DataTable from '@/components/ui/DataTable.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import PanelHeader from '@/components/ui/PanelHeader.vue'
import QueryFeedback from '@/components/ui/QueryFeedback.vue'
import SkeletonSurface from '@/components/ui/SkeletonSurface.vue'
import StatusBadge from '@/components/ui/StatusBadge.vue'

const props = defineProps<{ groupId: number }>()
const client = useApiClient()
const { t, n, locale } = useI18n()
const query = useQuery(groupModelUsageQueryOptions(client, () => props.groupId))
// 矩阵只下发 credential_id；可识别的标识来自该组凭据列表。后端只接受 page_size ≤ 100，
// 凭据可能有一页以上，因此按页取全后合并；任一分页失败即停止翻页，用已取到的部分建映射，
// 未覆盖的组合回退到内部 id，矩阵本身不受影响。
const credentialIdentityPageSize = 100
// 翻页硬上限：最多 10 页（1000 个凭据），避免凭据异常多时打出无限请求。
const credentialIdentityPageLimit = 10
async function loadCredentialMasks(signal: AbortSignal): Promise<Map<number, string>> {
  const map = new Map<number, string>()
  for (let page = 1; page <= credentialIdentityPageLimit; page += 1) {
    try {
      const collection = await getCredentialCollection(
        client,
        props.groupId,
        { page, page_size: credentialIdentityPageSize },
        signal,
      )
      for (const item of collection.items) map.set(item.credential_id, item.mask)
      if (page >= collection.pagination.total_pages) break
    } catch {
      // 任一页失败就保留已取到的部分，不向上抛错。
      break
    }
  }
  return map
}
const credentialsQuery = useQuery({
  queryKey: computed(() => controlQueryKeys.groups.credentialIdentities(props.groupId)),
  queryFn: ({ signal }: { signal: AbortSignal }) => loadCredentialMasks(signal),
  enabled: computed(() => props.groupId > 0),
})
const credentialMasks = computed(() => credentialsQuery.data.value ?? new Map<number, string>())
const initialLoading = useStableLoading(
  () => query.isPending.value && query.data.value === undefined,
)
const refreshing = computed(() => query.data.value !== undefined && query.isFetching.value)

type SortField = 'total_tokens' | 'request_count' | 'credential_id' | 'model'
const onlyCooling = ref(false)
const grouped = ref(false)
const sort = ref<SortField>('total_tokens')
const direction = ref<'asc' | 'desc'>('desc')
const collapsed = ref<Set<string>>(new Set())

const visible = computed(() =>
  (query.data.value?.items ?? []).filter(
    (row) => !onlyCooling.value || row.cooldown_until_ms !== null,
  ),
)
const rows = computed(() => {
  const factor = direction.value === 'asc' ? 1 : -1
  return [...visible.value].sort((left, right) => {
    if (sort.value === 'model') {
      const order = left.model.localeCompare(right.model)
      return order !== 0 ? factor * order : left.credential_id - right.credential_id
    }
    const difference =
      sort.value === 'credential_id'
        ? credentialOrder(left, right)
        : sort.value === 'request_count'
          ? left.request_count - right.request_count
          : left.total_tokens - right.total_tokens
    // 次要键固定为凭据与模型，保证同值行的顺序稳定可复现。
    return difference !== 0
      ? factor * difference
      : left.credential_id - right.credential_id || left.model.localeCompare(right.model)
  })
})
// 凭据列按用户在单元格里实际看到的标识排序；任一侧没有标识（凭据列表未加载或查不到）时
// 退回内部 id，与该列此前按 id 排序的行为一致。
function credentialOrder(left: GroupModelUsageItemDto, right: GroupModelUsageItemDto): number {
  const leftMask = credentialMasks.value.get(left.credential_id)
  const rightMask = credentialMasks.value.get(right.credential_id)
  return leftMask !== undefined && rightMask !== undefined
    ? leftMask.localeCompare(rightMask)
    : left.credential_id - right.credential_id
}
// 分组折叠只改变呈现分组，不改变行顺序；组内沿用传入顺序。
const modelGroups = computed(() => {
  const groups = new Map<string, GroupModelUsageItemDto[]>()
  for (const row of rows.value) {
    const bucket = groups.get(row.model)
    if (bucket) bucket.push(row)
    else groups.set(row.model, [row])
  }
  return [...groups].map(([model, groupRows]) => ({ model, rows: groupRows }))
})
const truncated = computed(() => {
  const data = query.data.value
  return data?.pagination !== undefined && data.pagination.total_items > data.items.length
})

function toggleSort(field: SortField): void {
  if (sort.value === field) {
    direction.value = direction.value === 'desc' ? 'asc' : 'desc'
    return
  }
  sort.value = field
  // 数值列默认从高到低，标识列默认升序。
  direction.value = field === 'credential_id' || field === 'model' ? 'asc' : 'desc'
}
function ariaSort(field: SortField): 'ascending' | 'descending' | 'none' {
  if (sort.value !== field) return 'none'
  return direction.value === 'asc' ? 'ascending' : 'descending'
}
function toggleModel(model: string): void {
  const next = new Set(collapsed.value)
  if (next.has(model)) next.delete(model)
  else next.add(model)
  collapsed.value = next
}
function tokensDetail(row: GroupModelUsageItemDto): string {
  const cacheWrite =
    row.cache_write_5m_tokens + row.cache_write_1h_tokens + row.cache_write_unknown_tokens
  return [
    t('group.usage.tokensDetail.label', {
      input: n(row.uncached_input_tokens + row.cache_read_tokens + cacheWrite),
      output: n(row.output_tokens),
    }),
    t('group.usage.tokensDetail.cache', {
      read: n(row.cache_read_tokens),
      write: n(cacheWrite),
    }),
  ].join('\n')
}
function windowDetail(row: GroupModelUsageItemDto): string {
  return [
    t('group.usage.windowStart', { time: formatLocalTime(row.window_start_ms, locale.value) }),
    row.window_source === 'reset'
      ? t('group.usage.window.exact')
      : t('group.usage.window.fallback'),
  ].join('\n')
}
function cooldownDetail(row: GroupModelUsageItemDto): string {
  if (row.cooldown_until_ms === null) return t('group.usage.none')
  return t('group.usage.cooldownUntil', {
    time: formatLocalTime(row.cooldown_until_ms, locale.value),
    remaining: formatRelativeInstant(row.cooldown_until_ms, Date.now(), locale.value),
  })
}
function remaining(row: GroupModelUsageItemDto): string {
  return row.cooldown_until_ms === null
    ? ''
    : formatRelativeInstant(
        row.cooldown_until_ms,
        query.data.value?.observed_at_ms ?? 0,
        locale.value,
      )
}
function countedRange(): string {
  const data = query.data.value
  if (!data) return ''
  return t('group.usage.hint', {
    from: formatLocalTime(data.counted_from_ms, locale.value),
    to: formatLocalTime(data.counted_to_ms, locale.value),
  })
}
function credentialIDLabel(row: GroupModelUsageItemDto): string {
  return t('group.usage.credentialLabel', { id: n(row.credential_id) })
}
function credentialIdentity(row: GroupModelUsageItemDto): string {
  return credentialMasks.value.get(row.credential_id) || credentialIDLabel(row)
}
function credentialHint(row: GroupModelUsageItemDto): string {
  const identity = credentialIdentity(row)
  const idLabel = credentialIDLabel(row)
  return identity === idLabel ? idLabel : `${identity}\n${idLabel}`
}
</script>

<template>
  <section class="group-usage" aria-labelledby="group-model-usage-heading">
    <PanelHeader heading-id="group-model-usage-heading" :title="t('group.usage.title')">
      <template #actions>
        <AppButton
          variant="secondary"
          size="compact"
          :busy="query.isFetching.value"
          :disabled="query.isPending.value"
          @click="query.refetch()"
        >
          <RefreshCw :size="16" aria-hidden="true" />{{ t('group.usage.refresh') }}
        </AppButton>
      </template>
    </PanelHeader>

    <AsyncRefreshIndicator :active="refreshing" :label="t('group.usage.loading')" />

    <SkeletonSurface
      v-if="(query.isPending.value && !query.data.value) || initialLoading"
      variant="collection"
      :rows="8"
      :columns="6"
      row-height="46px"
      mobile-row-height="96px"
      show-controls
      :show-pagination="false"
      :concealed="!initialLoading"
      :label="t('group.usage.loading')"
    />
    <QueryFeedback
      v-else-if="query.isError.value && !query.data.value"
      state="error"
      :message="t('group.usage.loadFailed')"
      :retry-label="t('group.usage.retry')"
      @retry="query.refetch()"
    />
    <template v-else-if="query.data.value">
      <QueryFeedback
        v-if="query.isError.value"
        state="stale"
        :message="t('group.usage.stale')"
        :retry-label="t('group.usage.retry')"
        @retry="query.refetch()"
      />

      <div class="group-usage__toolbar">
        <span class="group-usage__toggle">
          <AppSwitch v-model="onlyCooling" :label="t('group.usage.onlyCooling')" />
          <span>{{ t('group.usage.onlyCooling') }}</span>
        </span>
        <span class="group-usage__toggle">
          <AppSwitch v-model="grouped" :label="t('group.usage.groupByModel')" />
          <span>{{ t('group.usage.groupByModel') }}</span>
        </span>
        <AppTooltip :content="`${t('group.usage.window.help')}\n${countedRange()}`">
          <span class="group-usage__range" tabindex="0">{{ countedRange() }}</span>
        </AppTooltip>
      </div>

      <p v-if="truncated" class="group-usage__notice">
        {{
          t('group.usage.truncated', {
            shown: n(query.data.value.items.length),
            total: n(query.data.value.pagination?.total_items ?? 0),
          })
        }}
      </p>

      <EmptyState
        v-if="!rows.length"
        :title="onlyCooling ? t('group.usage.emptyFilterTitle') : t('group.usage.emptyTitle')"
        :description="
          onlyCooling ? t('group.usage.emptyFilterDescription') : t('group.usage.emptyDescription')
        "
        heading-as="h3"
        variant="panel"
      />

      <DataTable
        v-else
        dense
        appearance="editorial"
        :caption="t('group.usage.caption')"
        :scroll-hint="t('group.usage.scrollHint')"
      >
        <thead>
          <tr>
            <th scope="col" :aria-sort="ariaSort('credential_id')">
              <AppButton variant="link" size="inline" @click="toggleSort('credential_id')">{{
                t('group.usage.columns.credential')
              }}</AppButton>
            </th>
            <th scope="col" :aria-sort="ariaSort('model')">
              <AppButton variant="link" size="inline" @click="toggleSort('model')">{{
                t('group.usage.columns.model')
              }}</AppButton>
            </th>
            <th scope="col" class="group-usage__number" :aria-sort="ariaSort('total_tokens')">
              <AppButton variant="link" size="inline" @click="toggleSort('total_tokens')">{{
                t('group.usage.columns.tokens')
              }}</AppButton>
            </th>
            <th
              scope="col"
              class="group-usage__number"
              data-column-priority="low"
              :aria-sort="ariaSort('request_count')"
            >
              <AppButton variant="link" size="inline" @click="toggleSort('request_count')">{{
                t('group.usage.columns.requests')
              }}</AppButton>
            </th>
            <th scope="col">
              <AppTooltip :content="t('group.usage.window.help')"
                ><span>{{ t('group.usage.columns.window') }}</span></AppTooltip
              >
            </th>
            <th scope="col">{{ t('group.usage.columns.cooldown') }}</th>
          </tr>
        </thead>
        <tbody
          v-for="group in grouped ? modelGroups : [{ model: '', rows }]"
          :key="`tbody-${group.model}`"
        >
          <tr v-if="grouped" class="group-usage__group-row">
            <th colspan="6" scope="colgroup">
              <AppButton
                variant="link"
                size="inline"
                :aria-expanded="!collapsed.has(group.model)"
                @click="toggleModel(group.model)"
              >
                <component
                  :is="collapsed.has(group.model) ? ChevronRight : ChevronDown"
                  :size="14"
                  aria-hidden="true"
                />
                <span class="group-usage__mono">{{ group.model }}</span>
                <span class="group-usage__group-count">{{
                  t('group.usage.groupCount', { count: n(group.rows.length) })
                }}</span>
              </AppButton>
            </th>
          </tr>
          <template v-if="!grouped || !collapsed.has(group.model)">
            <tr
              v-for="row in group.rows"
              :key="`${row.credential_id}:${row.model}`"
              class="group-usage__row"
            >
              <td class="group-usage__credential">
                <AppTooltip :content="credentialHint(row)">
                  <span tabindex="0">{{ credentialIdentity(row) }}</span>
                </AppTooltip>
              </td>
              <td class="group-usage__mono">{{ row.model }}</td>
              <td class="group-usage__number">
                <AppTooltip :content="tokensDetail(row)">
                  <span class="group-usage__mono" tabindex="0">{{
                    formatTokens(row.total_tokens, locale)
                  }}</span>
                </AppTooltip>
              </td>
              <td class="group-usage__number" data-column-priority="low">
                <span class="group-usage__mono">{{
                  formatInteger(row.request_count, locale)
                }}</span>
              </td>
              <td>
                <AppTooltip :content="windowDetail(row)">
                  <span tabindex="0">
                    <StatusBadge
                      :tone="row.window_source === 'reset' ? 'info' : 'neutral'"
                      size="compact"
                      >{{
                        row.window_source === 'reset'
                          ? t('group.usage.window.exact')
                          : t('group.usage.window.fallback')
                      }}</StatusBadge
                    >
                  </span>
                </AppTooltip>
              </td>
              <td>
                <span v-if="row.cooldown_until_ms === null">{{ t('group.usage.none') }}</span>
                <span v-else class="group-usage__cooldown">
                  <StatusBadge status="cooldown" size="compact">{{
                    t('group.usage.cooling')
                  }}</StatusBadge>
                  <AppTooltip :content="cooldownDetail(row)">
                    <time
                      class="group-usage__mono"
                      :datetime="formatISOInstant(row.cooldown_until_ms)"
                      >{{ formatLocalTime(row.cooldown_until_ms, locale) }}</time
                    >
                  </AppTooltip>
                  <small class="group-usage__remaining">{{ remaining(row) }}</small>
                </span>
              </td>
            </tr>
          </template>
        </tbody>
      </DataTable>
    </template>
  </section>
</template>

<style scoped>
.group-usage {
  display: grid;
  min-width: 0;
  gap: var(--space-4);
}
.group-usage__toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3) var(--space-5);
}
.group-usage__toggle {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  color: var(--color-text-muted);
  font-size: var(--text-body);
}
.group-usage__range {
  margin-left: auto;
  color: var(--color-text-faint);
  font-size: var(--text-sm);
}
.group-usage__notice {
  color: var(--color-text-muted);
  font-size: var(--text-body);
}
.group-usage__number {
  text-align: right;
}
.group-usage__credential,
.group-usage__mono {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
}
.group-usage__group-row th {
  background: var(--color-surface-sunken);
}
.group-usage__group-count {
  color: var(--color-text-faint);
  font-family: var(--font-sans);
  font-variant-numeric: normal;
  font-weight: 400;
}
.group-usage__cooldown {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
}
.group-usage__remaining {
  color: var(--color-text-faint);
  font-size: var(--text-sm);
}
</style>
