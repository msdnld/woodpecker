<template>
  <div class="flex flex-col gap-y-4">
    <Panel :title="$t('repo.pipeline.run_info.pipeline_title')">
      <dl class="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-2 text-sm">
        <RunInfoRow :label="$t('repo.pipeline.run_info.number')">{{ `#${pipeline.number}` }}</RunInfoRow>
        <RunInfoRow :label="$t('repo.pipeline.run_info.event')">{{ eventLabel }}</RunInfoRow>
        <RunInfoRow :label="$t('repo.pipeline.run_info.status')">
          <span class="inline-flex items-center gap-1">
            <PipelineStatusIcon :status="pipeline.status" class="h-4 w-4" />
            {{ statusLabel(pipeline.status) }}
          </span>
        </RunInfoRow>
        <RunInfoRow :label="$t('repo.pipeline.run_info.branch')">{{ pipeline.branch }}</RunInfoRow>
        <RunInfoRow v-if="pipeline.ref" :label="$t('repo.pipeline.run_info.ref')">{{ pipeline.ref }}</RunInfoRow>
        <RunInfoRow :label="$t('repo.pipeline.run_info.commit')">
          <a v-if="pipeline.forge_url" :href="pipeline.forge_url" target="_blank" class="font-mono hover:underline">
            {{ pipeline.commit.slice(0, 10) }}
          </a>
          <span v-else class="font-mono">{{ pipeline.commit.slice(0, 10) }}</span>
        </RunInfoRow>
        <RunInfoRow :label="$t('repo.pipeline.run_info.message')">{{ pipeline.message }}</RunInfoRow>
        <RunInfoRow :label="$t('repo.pipeline.run_info.author')">{{ pipeline.author }}</RunInfoRow>
        <RunInfoRow v-if="pipeline.deploy_to" :label="$t('repo.pipeline.run_info.deploy_to')">
          {{ pipeline.deploy_to }}
        </RunInfoRow>
        <RunInfoRow v-if="pipeline.created" :label="$t('repo.pipeline.run_info.created')">
          {{ formatDate(pipeline.created) }}
        </RunInfoRow>
        <RunInfoRow v-if="pipeline.started" :label="$t('repo.pipeline.run_info.started')">
          {{ formatDate(pipeline.started) }}
        </RunInfoRow>
        <RunInfoRow v-if="pipeline.finished" :label="$t('repo.pipeline.run_info.finished')">
          {{ formatDate(pipeline.finished) }}
        </RunInfoRow>
      </dl>
    </Panel>

    <Panel
      v-if="dispatchInputs.length > 0 || extraVariables.length > 0"
      :title="$t('repo.pipeline.run_info.inputs_title')"
    >
      <template v-if="dispatchInputs.length > 0">
        <h4 class="text-wp-text-100 mb-2 font-bold">{{ $t('repo.pipeline.run_info.inputs') }}</h4>
        <dl class="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-2 text-sm">
          <RunInfoRow v-for="[name, value] in dispatchInputs" :key="name" :label="name">
            <span class="font-mono break-all">{{ value }}</span>
          </RunInfoRow>
        </dl>
      </template>
      <template v-if="extraVariables.length > 0">
        <h4 class="text-wp-text-100 mb-2 font-bold" :class="{ 'mt-4': dispatchInputs.length > 0 }">
          {{ $t('repo.pipeline.run_info.variables') }}
        </h4>
        <dl class="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-2 text-sm">
          <RunInfoRow v-for="[name, value] in extraVariables" :key="name" :label="name">
            <span class="font-mono break-all">{{ value }}</span>
          </RunInfoRow>
        </dl>
      </template>
    </Panel>

    <Panel
      v-for="workflow in pipeline.workflows ?? []"
      :key="workflow.id"
      :title="$t('repo.pipeline.run_info.workflow_title', { name: workflow.name })"
    >
      <dl class="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-2 text-sm">
        <RunInfoRow :label="$t('repo.pipeline.run_info.status')">
          <span class="inline-flex items-center gap-1">
            <PipelineStatusIcon :status="workflow.state" class="h-4 w-4" />
            {{ statusLabel(workflow.state) }}
          </span>
        </RunInfoRow>
        <RunInfoRow :label="$t('repo.pipeline.run_info.agent')">
          <template v-if="workflow.agent_id">
            {{ agentLabel(workflow.agent_id) }}
          </template>
          <span v-else class="text-wp-text-alt-100">{{ $t('repo.pipeline.run_info.no_agent') }}</span>
        </RunInfoRow>
        <RunInfoRow v-if="workflow.started" :label="$t('repo.pipeline.run_info.started')">
          {{ formatDate(workflow.started) }}
        </RunInfoRow>
        <RunInfoRow v-if="workflow.finished" :label="$t('repo.pipeline.run_info.finished')">
          {{ formatDate(workflow.finished) }}
        </RunInfoRow>
        <RunInfoRow v-if="workflow.error" :label="$t('repo.pipeline.run_info.error')">
          <span class="text-wp-error-100">{{ workflow.error }}</span>
        </RunInfoRow>
      </dl>

      <template v-if="workflow.environ && Object.keys(workflow.environ).length > 0">
        <h4 class="text-wp-text-100 mt-4 mb-2 font-bold">{{ $t('repo.pipeline.run_info.parameters') }}</h4>
        <dl class="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-2 text-sm">
          <RunInfoRow v-for="(value, key) in workflow.environ" :key="key" :label="key">
            <span class="font-mono break-all">{{ value }}</span>
          </RunInfoRow>
        </dl>
      </template>
    </Panel>
  </div>
</template>

<script lang="ts" setup>
import { computed, onMounted, ref } from 'vue';
import { useI18n } from 'vue-i18n';

import PipelineStatusIcon from '~/components/repo/pipeline/PipelineStatusIcon.vue';
import RunInfoRow from '~/components/repo/pipeline/RunInfoRow.vue';
import Panel from '~/components/layout/Panel.vue';
import useApiClient from '~/compositions/useApiClient';
import { useDate } from '~/compositions/useDate';
import { requiredInject } from '~/compositions/useInjectProvide';
import { useWPTitle } from '~/compositions/useWPTitle';
import type { Agent, PipelineStatus } from '~/lib/api/types';

const { t } = useI18n();
const apiClient = useApiClient();
const { toLocaleString } = useDate();

const repo = requiredInject('repo');
const pipeline = requiredInject('pipeline');

const agents = ref<Record<number, Agent>>({});

function formatDate(timestamp: number): string {
  return toLocaleString(new Date(timestamp * 1000));
}

// Typed workflow_dispatch inputs are injected as CI_INPUT_<NAME> variables;
// show them as the original input name, separate from plain run variables.
const INPUT_PREFIX = 'CI_INPUT_';

const dispatchInputs = computed(() =>
  Object.entries(pipeline.value.variables ?? {})
    .filter(([key]) => key.startsWith(INPUT_PREFIX))
    .map(([key, value]) => [key.slice(INPUT_PREFIX.length).toLowerCase(), value] as [string, string]),
);

const extraVariables = computed(() =>
  Object.entries(pipeline.value.variables ?? {}).filter(([key]) => !key.startsWith(INPUT_PREFIX)),
);

function statusLabel(status: PipelineStatus): string {
  switch (status) {
    case 'blocked':
      return t('repo.pipeline.status.blocked');
    case 'declined':
      return t('repo.pipeline.status.declined');
    case 'error':
      return t('repo.pipeline.status.error');
    case 'failure':
      return t('repo.pipeline.status.failure');
    case 'killed':
      return t('repo.pipeline.status.killed');
    case 'pending':
      return t('repo.pipeline.status.pending');
    case 'running':
      return t('repo.pipeline.status.running');
    case 'skipped':
      return t('repo.pipeline.status.skipped');
    case 'canceled':
      return t('repo.pipeline.status.canceled');
    case 'started':
      return t('repo.pipeline.status.started');
    default:
      return t('repo.pipeline.status.success');
  }
}

const eventLabel = computed(() => {
  switch (pipeline.value.event) {
    case 'pull_request':
      return t('repo.pipeline.event.pr');
    case 'pull_request_closed':
      return t('repo.pipeline.event.pr_closed');
    case 'pull_request_metadata':
      return t('repo.pipeline.event.pr_metadata');
    case 'deployment':
      return t('repo.pipeline.event.deploy');
    case 'tag':
      return t('repo.pipeline.event.tag');
    case 'release':
      return t('repo.pipeline.event.release');
    case 'cron':
      return t('repo.pipeline.event.cron');
    case 'manual':
      return t('repo.pipeline.event.manual');
    default:
      return t('repo.pipeline.event.push');
  }
});

function agentLabel(agentId: number): string {
  const agent = agents.value[agentId];
  return agent ? `${agent.name} (#${agentId})` : `#${agentId}`;
}

onMounted(async () => {
  const agentIds = [
    ...new Set((pipeline.value.workflows ?? []).map((w) => w.agent_id).filter((id): id is number => !!id)),
  ];
  await Promise.all(
    agentIds.map(async (id) => {
      try {
        agents.value[id] = await apiClient.getAgent(id);
      } catch {
        // agent details require admin permissions; fall back to showing the id only
      }
    }),
  );
});

useWPTitle(
  computed(() => [
    t('repo.pipeline.run_info.title'),
    t('repo.pipeline.pipeline', { pipelineId: pipeline.value.number }),
    repo.value.full_name,
  ]),
);
</script>
