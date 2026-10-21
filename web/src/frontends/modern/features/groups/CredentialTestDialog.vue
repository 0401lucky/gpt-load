<script setup lang="ts">
import { protocolLabel } from '@modern/i18n/protocols'
import { useLoadingActivity } from '@modern/components/ui/loading'
import { useQuery } from '@tanstack/vue-query'
import { DialogRoot } from 'reka-ui'
import { computed, onScopeDispose, ref, watch } from 'vue'
import { useMessageSource } from '@modern/app/messages'
import { useI18n } from 'vue-i18n'
import {
  getGroupModels,
  getGroupSettings,
  groupModelsKey,
  groupSettingsKey,
  type CredentialRow,
} from '@modern/api/group-detail'
import {
  restoreTestedCredential,
  testCredential,
  type CredentialTestResult,
} from '@modern/api/credential-actions'
import {
  AppProtocolTag,
  AppButton,
  AppDialogContent,
  AppDialogHeader,
  AppNotice,
  AppSearchSelect,
  AppSelect,
} from '@modern/components/ui'
import { useApiClient } from '@shared/http/client-context'
import type { CredentialTestOptions } from '@shared/credential-test-batch'
import AppDraftGuard from '@modern/components/AppDraftGuard.vue'
import { groupValidationModelOptions } from './group-model-options'
const props = defineProps<{ groupId: number; row?: CredentialRow; batchCount?: number }>()
const emit = defineEmits<{ close: []; changed: []; start: [options: CredentialTestOptions] }>()
const { t, n } = useI18n()
const title = computed(() =>
  t(props.batchCount ? 'credentialBatchTest.title' : 'credentialCards.test'),
)
const description = computed(() =>
  props.batchCount
    ? t('credentialBatchTest.description', { count: n(props.batchCount) })
    : (props.row?.label ?? ''),
)
const client = useApiClient()
const settings = useQuery({
  queryKey: groupSettingsKey(props.groupId),
  queryFn: ({ signal }) => getGroupSettings(client, props.groupId, signal),
})
const models = useQuery({
  queryKey: groupModelsKey(props.groupId),
  queryFn: ({ signal }) => getGroupModels(client, props.groupId, signal),
})
const model = ref('')
const protocol = ref('')
const pending = ref(false)
const result = ref<CredentialTestResult>()
const error = ref('')
let mounted = true
let requestOwner = 0
let requestController: AbortController | undefined
function isCurrent(owner: number, controller: AbortController): boolean {
  return (
    mounted &&
    requestOwner === owner &&
    requestController === controller &&
    !controller.signal.aborted
  )
}
let modelInitialized = false
let protocolInitialized = false
watch(
  [settings.data, models.data],
  ([value, items]) => {
    if (value && !protocolInitialized) {
      protocol.value = value.validationProtocol ?? value.validationProtocols[0] ?? ''
      protocolInitialized = true
    }
    if (value && !modelInitialized && (value.validationModel || items)) {
      model.value = value.validationModel ?? items?.[0]?.id ?? ''
      modelInitialized = true
    }
  },
  { immediate: true },
)
watch([model, protocol], () => {
  requestOwner++
  requestController?.abort()
  requestController = undefined
  pending.value = false
  result.value = undefined
  error.value = ''
})
watch(
  () => [props.groupId, props.row?.id, props.batchCount],
  () => {
    requestOwner++
    requestController?.abort()
    requestController = undefined
    pending.value = false
    result.value = undefined
    error.value = ''
  },
)
const protocols = computed(() =>
  (settings.data.value?.validationProtocols ?? []).map((value) => ({
    value,
    label: protocolLabel(value, t),
  })),
)
const modelOptions = computed(() => {
  const options = groupValidationModelOptions(models.data.value ?? [])
  const configured = settings.data.value?.validationModel
  return configured && !options.some((item) => item.value === configured)
    ? [{ value: configured, label: configured }, ...options]
    : options
})
function updateModel(value: string): void {
  modelInitialized = true
  model.value = value
}
function updateProtocol(value: string): void {
  protocolInitialized = true
  protocol.value = value
}
async function reloadSettings(): Promise<void> {
  await Promise.all([settings.refetch(), models.refetch()])
}
async function run(restore = false): Promise<void> {
  if (!mounted || pending.value || (!restore && (!model.value.trim() || !protocol.value))) return
  if (props.batchCount) {
    if (!restore) emit('start', { protocol: protocol.value, model: model.value.trim() })
    return
  }
  const credentialID = props.row?.id
  const proof = result.value?.proof
  if (!credentialID || (restore && !proof)) return
  const groupID = props.groupId
  const owner = ++requestOwner
  const controller = new AbortController()
  requestController = controller
  pending.value = true
  error.value = ''
  try {
    if (restore && proof) {
      await restoreTestedCredential(client, groupID, credentialID, proof, controller.signal)
      if (!isCurrent(owner, controller)) return
      result.value = undefined
      emit('changed')
      emit('close')
    } else {
      const tested = await testCredential(
        client,
        groupID,
        credentialID,
        protocol.value,
        model.value.trim(),
        controller.signal,
      )
      if (!isCurrent(owner, controller)) return
      result.value = tested
      emit('changed')
    }
  } catch {
    if (isCurrent(owner, controller)) error.value = t('credentialCards.actionFailed')
  } finally {
    if (isCurrent(owner, controller)) {
      requestController = undefined
      pending.value = false
    }
  }
}
function close(): void {
  if (!pending.value) emit('close')
}
onScopeDispose(() => {
  mounted = false
  requestOwner++
  requestController?.abort()
})
useLoadingActivity(pending)
useMessageSource(() => (error.value ? { text: error.value, tone: 'danger' } : undefined))
</script>
<template>
  <DialogRoot
    :open="true"
    @update:open="
      (value) => {
        if (!value) close()
      }
    "
  >
    <AppDialogContent :title="title" :description="description">
      <AppDialogHeader
        :title="title"
        :description="description"
        :close-label="t('shell.close')"
        :close-disabled="pending"
        @close="close"
      />
      <form class="modern-credential-test" @submit.prevent="run()">
        <AppSelect
          :model-value="protocol"
          :label="t('groupDetail.validationProtocol')"
          :options="protocols"
          :disabled="pending || settings.isPending.value || protocols.length === 1"
          @update:model-value="updateProtocol"
        >
          <template #value="{ value, label }"
            ><AppProtocolTag v-if="value" :protocol="value" /><span v-else>{{
              label
            }}</span></template
          >
          <template #option="{ option }"
            ><AppProtocolTag v-if="option.value" :protocol="option.value" /><span v-else>{{
              option.label
            }}</span></template
          >
        </AppSelect>
        <AppSearchSelect
          :model-value="model"
          :label="t('groupDetail.validationModel')"
          :options="modelOptions"
          allow-custom
          :disabled="pending || models.isPending.value"
          @update:model-value="updateModel"
        />
        <AppNotice v-if="settings.data.value && !protocols.length" tone="warning">{{
          t('credentialCards.noTestProtocol')
        }}</AppNotice>
        <AppNotice v-if="settings.isError.value || models.isError.value" tone="danger"
          >{{ t('groups.edit.loadFailed')
          }}<template #actions
            ><AppButton
              :disabled="pending || settings.isFetching.value || models.isFetching.value"
              @click="reloadSettings"
              >{{ t('ui.retry') }}</AppButton
            ></template
          ></AppNotice
        >
        <AppNotice
          v-if="result"
          :tone="
            result.outcome === 'passed'
              ? 'success'
              : result.outcome === 'failed'
                ? 'danger'
                : 'warning'
          "
          >{{ t('credentialCards.testResult.' + result.outcome) }} · {{ result.latency }} ms<span
            v-if="result.reason"
          >
            · {{ t('credentialCards.testReason.' + result.reason) }}</span
          ></AppNotice
        >
        <dl v-if="result" class="modern-credential-test-details">
          <template v-if="result.upstreamStatusCode !== null"
            ><dt>{{ t('credentialCards.upstreamStatus') }}</dt>
            <dd>{{ result.upstreamStatusCode }}</dd></template
          >
          <template v-if="result.outcome !== 'passed'"
            ><dt>{{ t('credentialCards.upstreamDetail') }}</dt>
            <dd>{{ result.errorSummary ?? t('credentialCards.noDetail') }}</dd></template
          >
          <template v-if="result.retryAfterSeconds !== null"
            ><dt>{{ t('credentialCards.retryAfter') }}</dt>
            <dd>
              {{ t('credentialCards.retrySeconds', { value: n(result.retryAfterSeconds) }) }}
            </dd></template
          >
        </dl>
        <div class="modern-credential-test-actions">
          <AppButton :disabled="pending" @click="close">{{ t('shell.close') }}</AppButton
          ><AppButton
            v-if="result?.proof"
            variant="outline"
            :loading="pending"
            @click="run(true)"
            >{{ t('credentialCards.restore') }}</AppButton
          ><AppButton
            type="submit"
            variant="primary"
            :loading="pending"
            :disabled="!model.trim() || !protocol || pending"
            >{{ t(batchCount ? 'credentialBatchTest.start' : 'credentialCards.test') }}</AppButton
          >
        </div>
      </form>
    </AppDialogContent>
  </DialogRoot>
  <AppDraftGuard :dirty="false" :pending="pending" />
</template>
<style scoped>
.modern-credential-test {
  position: relative;
  display: grid;
  gap: var(--modern-space-4);
  padding: var(--modern-space-5) var(--modern-space-6);
}
.modern-credential-test-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: var(--modern-space-2);
  margin-top: var(--modern-space-2);
}
.modern-credential-test-details {
  display: grid;
  grid-template-columns: max-content minmax(0, 1fr);
  gap: var(--modern-space-2) var(--modern-space-3);
  margin: 0;
  overflow-wrap: anywhere;
}
.modern-credential-test-details dd {
  min-width: 0;
  margin: 0;
}
</style>
