<!--
  Manual "workflow dispatch" view (GitHub-Actions style): pick a single workflow
  file, fill its typed inputs, and run it. Kept separate from
  RepoManualPipeline.vue so this fork stays easy to rebase against upstream.
-->
<template>
  <Panel v-if="!loading">
    <form @submit.prevent="triggerWorkflowDispatch">
      <span class="text-wp-text-100 text-xl">{{ $t('repo.workflow_dispatch.title') }}</span>
      <InputField v-slot="{ id }" :label="$t('repo.manual_pipeline.select_branch')">
        <SelectField
          :id="id"
          v-model="payload.branch"
          :options="branches"
          required
          @update:model-value="loadWorkflows"
        />
      </InputField>
      <InputField v-slot="{ id }" :label="$t('repo.workflow_dispatch.select_workflow')">
        <SelectField :id="id" v-model="payload.workflow" :options="workflowOptions" />
      </InputField>

      <!-- typed inputs declared by the selected workflow -->
      <template v-for="input in selectedInputs" :key="input.name">
        <InputField :label="inputLabel(input)">
          <span v-if="input.description" class="text-wp-text-alt-100 mb-2 text-sm">{{ input.description }}</span>
          <SelectField
            v-if="input.type === 'choice'"
            v-model="inputValues[input.name] as string"
            :options="choiceOptions(input)"
          />
          <Checkbox
            v-else-if="input.type === 'boolean'"
            :model-value="inputValues[input.name] as boolean"
            label=""
            @update:model-value="inputValues[input.name] = $event"
          />
          <NumberField
            v-else-if="input.type === 'number'"
            :model-value="inputValues[input.name] as number"
            @update:model-value="inputValues[input.name] = $event"
          />
          <TextField v-else v-model="inputValues[input.name] as string" />
        </InputField>
      </template>

      <InputField v-slot="{ id }" :label="$t('repo.manual_pipeline.variables.title')">
        <span class="text-wp-text-alt-100 mb-2 text-sm">{{ $t('repo.manual_pipeline.variables.desc') }}</span>
        <KeyValueEditor
          :id="id"
          v-model="payload.variables"
          :key-placeholder="$t('repo.manual_pipeline.variables.name')"
          :value-placeholder="$t('repo.manual_pipeline.variables.value')"
          :delete-title="$t('repo.manual_pipeline.variables.delete')"
          @update:is-valid="isVariablesValid = $event"
        />
      </InputField>
      <Button type="submit" :text="$t('repo.workflow_dispatch.trigger')" :disabled="!isFormValid" />
    </form>
  </Panel>
  <div v-else class="text-wp-text-100 flex justify-center">
    <Icon name="spinner" />
  </div>
</template>

<script lang="ts" setup>
import { useNotification } from '@kyvg/vue3-notification';
import { computed, onMounted, ref, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import { useRouter } from 'vue-router';

import Button from '~/components/atomic/Button.vue';
import Icon from '~/components/atomic/Icon.vue';
import Checkbox from '~/components/form/Checkbox.vue';
import type { SelectOption } from '~/components/form/form.types';
import InputField from '~/components/form/InputField.vue';
import KeyValueEditor from '~/components/form/KeyValueEditor.vue';
import NumberField from '~/components/form/NumberField.vue';
import SelectField from '~/components/form/SelectField.vue';
import TextField from '~/components/form/TextField.vue';
import Panel from '~/components/layout/Panel.vue';
import useApiClient from '~/compositions/useApiClient';
import { requiredInject } from '~/compositions/useInjectProvide';
import { usePaginate } from '~/compositions/usePaginate';
import { useWPTitle } from '~/compositions/useWPTitle';
import type { WorkflowInput, WorkflowMeta } from '~/lib/api/types';

const apiClient = useApiClient();
const notifications = useNotification();
const i18n = useI18n();

const repo = requiredInject('repo');
const repoPermissions = requiredInject('repo-permissions');

const router = useRouter();
const branches = ref<{ text: string; value: string }[]>([]);

// empty workflow value => run all workflows matching the event (current behavior)
const payload = ref<{ branch: string; workflow: string; variables: Record<string, string> }>({
  branch: 'main',
  workflow: '',
  variables: {},
});

const workflows = ref<WorkflowMeta[]>([]);
const inputValues = ref<Record<string, string | number | boolean>>({});

const workflowOptions = computed<SelectOption[]>(() => [
  { text: i18n.t('repo.workflow_dispatch.all_workflows'), value: '' },
  ...workflows.value.map((w) => ({ text: w.name, value: w.file })),
]);

const selectedInputs = computed<WorkflowInput[]>(
  () => workflows.value.find((w) => w.file === payload.value.workflow)?.inputs ?? [],
);

const isVariablesValid = ref(true);

const areInputsValid = computed(() =>
  selectedInputs.value.every((input) => {
    if (!input.required || input.type === 'boolean') {
      return true;
    }
    const value = inputValues.value[input.name];
    if (input.type === 'number') {
      return value !== '' && !Number.isNaN(value);
    }
    return value !== undefined && value !== null && String(value) !== '';
  }),
);

const isFormValid = computed(() => payload.value.branch !== '' && isVariablesValid.value && areInputsValid.value);

const loading = ref(true);

function inputLabel(input: WorkflowInput): string {
  return input.required ? `${input.name} *` : input.name;
}

function choiceOptions(input: WorkflowInput): SelectOption[] {
  return (input.options ?? []).map((o) => ({ text: o, value: o }));
}

function defaultFor(input: WorkflowInput): string | number | boolean {
  switch (input.type) {
    case 'boolean':
      return input.default === true || input.default === 'true';
    case 'number':
      return input.default !== undefined && input.default !== null ? Number(input.default) : 0;
    case 'choice':
      return String(input.default ?? input.options?.[0] ?? '');
    default:
      return input.default !== undefined && input.default !== null ? String(input.default) : '';
  }
}

// reset the input form to the selected workflow's declared defaults
function resetInputs() {
  const values: Record<string, string | number | boolean> = {};
  for (const input of selectedInputs.value) {
    values[input.name] = defaultFor(input);
  }
  inputValues.value = values;
}

watch(() => payload.value.workflow, resetInputs);

async function loadWorkflows() {
  workflows.value = await apiClient.getWorkflows(repo.value.id, payload.value.branch);
  // reset selection if the previously chosen workflow no longer exists on this branch
  if (!workflows.value.some((w) => w.file === payload.value.workflow)) {
    payload.value.workflow = '';
  }
  resetInputs();
}

onMounted(async () => {
  if (!repoPermissions.value.push) {
    notifications.notify({ type: 'error', title: i18n.t('repo.settings.not_allowed') });
    await router.replace({ name: 'home' });
    return;
  }

  const data = await usePaginate((page) => apiClient.getRepoBranches(repo.value.id, { page }));
  branches.value = data.map((e) => ({ text: e, value: e }));
  if (branches.value.length > 0 && !branches.value.some((b) => b.value === payload.value.branch)) {
    payload.value.branch = branches.value[0].value;
  }

  await loadWorkflows();
  loading.value = false;
});

async function triggerWorkflowDispatch() {
  loading.value = true;
  const pipeline = await apiClient.createPipeline(repo.value.id, {
    branch: payload.value.branch,
    variables: payload.value.variables,
    workflows: payload.value.workflow ? [payload.value.workflow] : undefined,
    inputs: selectedInputs.value.length > 0 ? { ...inputValues.value } : undefined,
  });

  if (typeof pipeline === 'string') {
    // http 204: no workflow matched the manual event
    await router.push({ name: 'repo' });
    notifications.notify({ type: 'warn', title: i18n.t('repo.manual_pipeline.no_manual_workflows') });
  } else {
    await router.push({ name: 'repo-pipeline', params: { pipelineId: pipeline.number } });
  }

  loading.value = false;
}

useWPTitle(computed(() => [i18n.t('repo.workflow_dispatch.trigger'), repo.value.full_name]));
</script>
