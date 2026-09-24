import { queryOptions } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'

import type { ApiClient } from '@shared/http/client'
import type { GroupModelUsageDto, GroupModelUsageItemDto } from '@/api/control/types'
import { InvalidResponseError } from '@shared/http/errors'
import { controlQueryKeys } from '@/app/query-keys'

import {
  assertNoSecretLikeFields,
  projectArray,
  projectEnum,
  projectEpochMilliseconds,
  projectNullableEpochMilliseconds,
  projectRecord,
  projectSafeInteger,
  projectString,
} from './projector'

export type {
  GroupModelUsageDto,
  GroupModelUsageItemDto,
  GroupModelWindowSource,
} from '@/api/control/types'

const groupModelUsageFields = [
  'observed_at_ms',
  'counted_from_ms',
  'counted_to_ms',
  'items',
  'pagination',
] as const
const groupModelUsageItemFields = [
  'credential_id',
  'model',
  'window_start_ms',
  'window_source',
  'cooldown_until_ms',
  'request_count',
  'success_count',
  'failure_count',
  'uncached_input_tokens',
  'cache_read_tokens',
  'cache_write_5m_tokens',
  'cache_write_1h_tokens',
  'cache_write_unknown_tokens',
  'output_tokens',
  'total_tokens',
] as const
const groupModelUsagePaginationFields = ['page', 'page_size', 'total_items', 'total_pages'] as const
const groupModelWindowSources = ['reset', 'fallback_24h'] as const

function projectGroupModelUsageItem(value: unknown): GroupModelUsageItemDto {
  const record = projectRecord(value)
  assertNoSecretLikeFields(record, groupModelUsageItemFields)
  return {
    credential_id: projectSafeInteger(record.credential_id, { minimum: 1 }),
    model: projectString(record.model),
    window_start_ms: projectEpochMilliseconds(record.window_start_ms),
    window_source: projectEnum(record.window_source, groupModelWindowSources),
    cooldown_until_ms: projectNullableEpochMilliseconds(record.cooldown_until_ms),
    request_count: projectSafeInteger(record.request_count, { minimum: 0 }),
    success_count: projectSafeInteger(record.success_count, { minimum: 0 }),
    failure_count: projectSafeInteger(record.failure_count, { minimum: 0 }),
    uncached_input_tokens: projectSafeInteger(record.uncached_input_tokens, { minimum: 0 }),
    cache_read_tokens: projectSafeInteger(record.cache_read_tokens, { minimum: 0 }),
    cache_write_5m_tokens: projectSafeInteger(record.cache_write_5m_tokens, { minimum: 0 }),
    cache_write_1h_tokens: projectSafeInteger(record.cache_write_1h_tokens, { minimum: 0 }),
    cache_write_unknown_tokens: projectSafeInteger(record.cache_write_unknown_tokens, {
      minimum: 0,
    }),
    output_tokens: projectSafeInteger(record.output_tokens, { minimum: 0 }),
    total_tokens: projectSafeInteger(record.total_tokens, { minimum: 0 }),
  }
}

export function projectGroupModelUsage(value: unknown): GroupModelUsageDto {
  const record = projectRecord(value)
  assertNoSecretLikeFields(record, groupModelUsageFields)
  const items = projectArray(record.items, projectGroupModelUsageItem)
  if (record.pagination === undefined) {
    return {
      observed_at_ms: projectEpochMilliseconds(record.observed_at_ms),
      counted_from_ms: projectEpochMilliseconds(record.counted_from_ms),
      counted_to_ms: projectEpochMilliseconds(record.counted_to_ms),
      items,
    }
  }
  const pagination = projectRecord(record.pagination)
  assertNoSecretLikeFields(pagination, groupModelUsagePaginationFields)
  // 截断时后端以 total_items 标明总行数；缺失时按已下发的行数处理。
  const totalItems =
    pagination.total_items === undefined
      ? items.length
      : projectSafeInteger(pagination.total_items, { minimum: 0 })
  if (totalItems < items.length) throw new InvalidResponseError()
  return {
    observed_at_ms: projectEpochMilliseconds(record.observed_at_ms),
    counted_from_ms: projectEpochMilliseconds(record.counted_from_ms),
    counted_to_ms: projectEpochMilliseconds(record.counted_to_ms),
    items,
    pagination: { total_items: totalItems },
  }
}

export async function getGroupModelUsage(
  client: ApiClient,
  groupID: number,
  signal?: AbortSignal,
): Promise<GroupModelUsageDto> {
  return projectGroupModelUsage(
    await client.request(`/api/groups/${groupID}/model-usage`, { method: 'GET', signal }),
  )
}

export function groupModelUsageQueryOptions(
  client: ApiClient,
  groupID: MaybeRefOrGetter<number | undefined>,
) {
  return queryOptions({
    // 用量随请求持续变化，进入分区时总是重新取数。
    refetchOnMount: 'always',
    staleTime: Number.POSITIVE_INFINITY,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    queryKey: computed(() => {
      const id = toValue(groupID)
      return id === undefined
        ? controlQueryKeys.groups.modelUsageAll()
        : controlQueryKeys.groups.modelUsage(id)
    }),
    queryFn: ({ signal }) => {
      const id = toValue(groupID)
      if (id === undefined) throw new InvalidResponseError()
      return getGroupModelUsage(client, id, signal)
    },
    enabled: computed(() => toValue(groupID) !== undefined),
  })
}
