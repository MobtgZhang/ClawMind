<script setup lang="ts">
/** Lucide sprite 图标（frontend/public/sprite.svg，ISC 许可见同目录 LICENSE.lucide.txt）。
 *  与 Maya 一致：一个 <use> 引用，尺寸由 st-icon 变体类决定。 */
withDefaults(
  defineProps<{
    name: string;
    size?: "sm" | "lg";
    cls?: string;
  }>(),
  { size: undefined, cls: "" }
);
</script>

<template>
  <svg
    class="st-icon"
    :class="[size === 'sm' ? 'st-icon--sm' : '', size === 'lg' ? 'st-icon--lg' : '', cls]"
    aria-hidden="true"
  >
    <use :href="`/sprite.svg#i-${name}`" />
  </svg>
</template>
