<!--
  Manual "workflow dispatch" view (GitHub-Actions style): pick a single workflow
  file and run it. Kept separate from RepoManualPipeline.vue so this fork stays
  easy to rebase against upstream.
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
import { computed, onMounted, ref } from 'vue';
import { useI18n } from 'vue-i18n';
import { useRouter } from 'vue-router';

import Button from '~/components/atomic/Button.vue';
import Icon from '~/components/atomic/Icon.vue';
import InputField from '~/components/form/InputField.vue';
import KeyValueEditor from '~/components/form/KeyValueEditor.vue';
import SelectField from '~/components/form/SelectField.vue';
import Panel from '~/components/layout/Panel.vue';
import useApiClient from '~/compositions/useApiClient';
import { requiredInject } from '~/compositions/useInjectProvide';
import { usePaginate } from '~/compositions/usePaginate';
import { useWPTitle } from '~/compositions/useWPTitle';

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

const workflowOptions = ref<{ text: string; value: string }[]>([
  { text: i18n.t('repo.workflow_dispatch.all_workflows'), value: '' },
]);

const isVariablesValid = ref(true);

const isFormValid = computed(() => payload.value.branch !== '' && isVariablesValid.value);

const loading = ref(true);

async function loadWorkflows() {
  const workflows = await apiClient.getWorkflows(repo.value.id, payload.value.branch);
  workflowOptions.value = [
    { text: i18n.t('repo.workflow_dispatch.all_workflows'), value: '' },
    ...workflows.map((w) => ({ text: w.name, value: w.file })),
  ];
  // reset selection if the previously chosen workflow no longer exists on this branch
  if (!workflowOptions.value.some((o) => o.value === payload.value.workflow)) {
    payload.value.workflow = '';
  }
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
