<template>
  <div class="space-y-4">
    <div v-if="workflowOptions.length > 1" class="flex items-center gap-2">
      <label :for="filterId" class="text-wp-text-100 text-sm">{{ $t('repo.pipeline.filter_workflow') }}</label>
      <div class="w-full max-w-xs">
        <SelectField :id="filterId" v-model="workflowFilter" :options="workflowOptions" />
      </div>
    </div>
    <PipelineList
      :pipelines="shownPipelines"
      :loading="loading"
      :has-more="hasMore"
      :empty-text="emptyText"
      @load-more="loadMore"
    />
  </div>
</template>

<script lang="ts" setup>
import { computed, onMounted, ref, useId, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import { useRoute, useRouter } from 'vue-router';

import SelectField from '~/components/form/SelectField.vue';
import PipelineList from '~/components/repo/pipeline/PipelineList.vue';
import useApiClient from '~/compositions/useApiClient';
import { requiredInject } from '~/compositions/useInjectProvide';
import { useWPTitle } from '~/compositions/useWPTitle';
import { usePipelineStore } from '~/store/pipelines';

// TODO(4626): Refactor to use usePagination with server-side filtering,
// so pipeline loading can move from RepoWrapper to individual list views.
const repo = requiredInject('repo');
const repoPermissions = requiredInject('repo-permissions');
const pipelines = requiredInject('pipelines');
const pipelineStore = usePipelineStore();
const apiClient = useApiClient();
const route = useRoute();
const router = useRouter();
const { t } = useI18n();

const perPage = 50;
const filterId = useId();

// The selected workflow lives in the URL so a filtered list survives reloads
// and can be shared as a link.
const workflowFilter = computed<string>({
  get: () => (typeof route.query.workflow === 'string' ? route.query.workflow : ''),
  set: (workflow) => {
    const query = { ...route.query };
    if (workflow) {
      query.workflow = workflow;
    } else {
      delete query.workflow;
    }
    router.replace({ query });
  },
});

// Workflows known from the repo config (needs push permission) plus every
// workflow seen on already loaded pipelines, so old runs stay filterable.
const configWorkflows = ref<string[]>([]);
const workflowOptions = computed(() => {
  const names = new Set(configWorkflows.value);
  for (const pipeline of pipelines.value) {
    if (pipeline.selected_workflow) {
      names.add(pipeline.selected_workflow);
    }
  }
  if (workflowFilter.value) {
    names.add(workflowFilter.value);
  }
  return [
    { value: '', text: t('repo.pipeline.all_workflows') },
    ...[...names].sort((a, b) => a.localeCompare(b)).map((name) => ({ value: name, text: name })),
  ];
});

const shownPipelines = computed(() => {
  if (!workflowFilter.value) {
    return pipelines.value;
  }
  return pipelines.value.filter((pipeline) => pipeline.selected_workflow === workflowFilter.value);
});

const emptyText = computed(() =>
  workflowFilter.value
    ? t('repo.pipeline.no_pipelines_for_workflow', { workflow: workflowFilter.value })
    : t('repo.pipeline.no_pipelines'),
);

// Filtered pages are fetched separately from the unfiltered feed and merged
// into the store, so live updates keep working for filtered pipelines too.
const page = ref(1);
const filteredPage = ref(1);
const filteredHasMore = ref(false);
const filteredLoading = ref(false);

const loading = computed(() => (workflowFilter.value ? filteredLoading.value : pipelineStore.loading));
const hasMore = computed(() => (workflowFilter.value ? filteredHasMore.value : pipelineStore.hasMore));

async function loadFilteredPipelines(workflow: string, pageToLoad: number) {
  filteredLoading.value = true;
  try {
    const loaded = await apiClient.getPipelineList(repo.value.id, { page: pageToLoad, perPage, workflow });
    loaded.forEach((pipeline) => pipelineStore.setPipeline(repo.value.id, pipeline));
    filteredHasMore.value = loaded.length >= perPage;
  } finally {
    filteredLoading.value = false;
  }
}

async function loadMore() {
  if (workflowFilter.value) {
    filteredPage.value += 1;
    await loadFilteredPipelines(workflowFilter.value, filteredPage.value);
    return;
  }
  page.value += 1;
  await pipelineStore.loadRepoPipelines(repo.value.id, page.value);
}

watch(
  workflowFilter,
  async (workflow) => {
    filteredPage.value = 1;
    filteredHasMore.value = false;
    if (workflow) {
      await loadFilteredPipelines(workflow, 1);
    }
  },
  { immediate: true },
);

onMounted(async () => {
  if (!repoPermissions.value.push) {
    return;
  }
  try {
    const workflows = await apiClient.getRepoWorkflows(repo.value.id, repo.value.default_branch);
    configWorkflows.value = workflows.map((workflow) => workflow.name);
  } catch {
    configWorkflows.value = [];
  }
});

useWPTitle(computed(() => [t('repo.activity'), repo.value.full_name]));
</script>
