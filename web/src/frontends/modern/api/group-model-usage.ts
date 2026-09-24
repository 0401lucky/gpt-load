import type { ApiClient } from '@shared/http/client'
import { InvalidResponseError } from '@shared/http/errors'
import { integer, list, oneOf, record, text } from './response'

export const groupModelUsageKey = (id: number) => ['modern', 'group-model-usage', id] as const
// 窗口口径：reset 为该 (凭据, 上游模型) 自身周期的精确窗口，fallback_24h 为无周期记录时的近 24 小时兜底。
export const windowSources = ['reset', 'fallback_24h'] as const
export type WindowSource = (typeof windowSources)[number]

export interface GroupModelUsageRow {
  credentialID: number
  model: string
  windowStart: number
  windowSource: WindowSource
  cooldownUntil: number | null
  requestCount: number
  successCount: number
  failureCount: number
  uncachedInputTokens: number
  cacheReadTokens: number
  cacheWrite5mTokens: number
  cacheWrite1hTokens: number
  cacheWriteUnknownTokens: number
  outputTokens: number
  totalTokens: number
}

export interface GroupModelUsage {
  observedAt: number
  countedFrom: number
  countedTo: number
  items: GroupModelUsageRow[]
  totalItems: number
  truncated: boolean
}

function readRow(value: unknown): GroupModelUsageRow {
  const row = record(value)
  return {
    credentialID: integer(row.credential_id, 1),
    model: text(row.model),
    windowStart: integer(row.window_start_ms),
    windowSource: oneOf(row.window_source, windowSources),
    cooldownUntil: row.cooldown_until_ms === null ? null : integer(row.cooldown_until_ms),
    requestCount: integer(row.request_count),
    successCount: integer(row.success_count),
    failureCount: integer(row.failure_count),
    uncachedInputTokens: integer(row.uncached_input_tokens),
    cacheReadTokens: integer(row.cache_read_tokens),
    cacheWrite5mTokens: integer(row.cache_write_5m_tokens),
    cacheWrite1hTokens: integer(row.cache_write_1h_tokens),
    cacheWriteUnknownTokens: integer(row.cache_write_unknown_tokens),
    outputTokens: integer(row.output_tokens),
    totalTokens: integer(row.total_tokens),
  }
}

export function readGroupModelUsage(value: unknown): GroupModelUsage {
  const data = record(value)
  const items = list(data.items).map(readRow)
  // 截断信息按既有列表的 pagination 形态下发，字段可能整体缺失。
  const totalItems =
    data.pagination === undefined ? items.length : integer(record(data.pagination).total_items)
  if (totalItems < items.length) throw new InvalidResponseError()
  return {
    observedAt: integer(data.observed_at_ms),
    countedFrom: integer(data.counted_from_ms),
    countedTo: integer(data.counted_to_ms),
    items,
    totalItems,
    truncated: totalItems > items.length,
  }
}

export async function getGroupModelUsage(
  client: ApiClient,
  id: number,
  signal: AbortSignal,
): Promise<GroupModelUsage> {
  return readGroupModelUsage(
    await client.request(`/api/groups/${id}/model-usage`, {
      method: 'GET',
      signal,
    }),
  )
}

export type ModelUsageSort = 'total_tokens' | 'request_count' | 'credential_id' | 'model'
export type ModelUsageDirection = 'asc' | 'desc'

export function sortModelUsage(
  rows: readonly GroupModelUsageRow[],
  sort: ModelUsageSort,
  direction: ModelUsageDirection,
  credentialLabel?: (credentialID: number) => string | undefined,
): GroupModelUsageRow[] {
  const factor = direction === 'asc' ? 1 : -1
  return [...rows].sort((left, right) => {
    if (sort === 'model') {
      const order = left.model.localeCompare(right.model)
      return order !== 0 ? factor * order : left.credentialID - right.credentialID
    }
    const difference =
      sort === 'credential_id'
        ? credentialOrder(left, right, credentialLabel)
        : sort === 'request_count'
          ? left.requestCount - right.requestCount
          : left.totalTokens - right.totalTokens
    // 次要键固定为凭据与模型，保证同值行的顺序稳定可复现。
    return difference !== 0
      ? factor * difference
      : left.credentialID - right.credentialID || left.model.localeCompare(right.model)
  })
}

// 凭据列按用户在单元格里实际看到的标识排序；任一侧没有标识（凭据列表未加载或查不到）时
// 退回内部 id，与该列此前按 id 排序的行为一致。
function credentialOrder(
  left: GroupModelUsageRow,
  right: GroupModelUsageRow,
  credentialLabel?: (credentialID: number) => string | undefined,
): number {
  const leftLabel = credentialLabel?.(left.credentialID)
  const rightLabel = credentialLabel?.(right.credentialID)
  return leftLabel !== undefined && rightLabel !== undefined
    ? leftLabel.localeCompare(rightLabel)
    : left.credentialID - right.credentialID
}

// 分组折叠只改变呈现分组，不改变行顺序；组内沿用传入顺序。
export function groupRowsByModel(
  rows: readonly GroupModelUsageRow[],
): { model: string; rows: GroupModelUsageRow[] }[] {
  const groups = new Map<string, GroupModelUsageRow[]>()
  for (const row of rows) {
    const bucket = groups.get(row.model)
    if (bucket) bucket.push(row)
    else groups.set(row.model, [row])
  }
  return [...groups].map(([model, groupRows]) => ({ model, rows: groupRows }))
}
