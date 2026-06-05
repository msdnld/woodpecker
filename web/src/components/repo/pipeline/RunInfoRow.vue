<template>
  <dt class="text-wp-text-alt-100 break-all">{{ label }}</dt>
  <dd class="min-w-0 break-words">
    <slot />
  </dd>
</template>

<script lang="ts" setup>
defineProps<{
  label: string;
}>();
</script>
