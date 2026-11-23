<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { X } from "@lucide/vue";
import { formatPower, type Lang } from "@/format";
import type { SiteModel } from "@/api/types";
import { directed, meterLabel, type Lookup, type RolePower } from "./power";

const props = defineProps<{
  node: RolePower | null;
  label: string;
  site: SiteModel;
  get: Lookup;
}>();
const emit = defineEmits<{ close: [] }>();
const { t, locale } = useI18n();
const dialog = ref<HTMLDialogElement | null>(null);

watch(
  () => props.node,
  (n) => {
    if (n && !dialog.value?.open) dialog.value?.showModal();
    if (!n && dialog.value?.open) dialog.value.close();
  },
);

const rows = computed(() =>
  (props.node?.meters ?? []).map((id) => ({
    id,
    label: meterLabel(props.site, id, {
      backup: t("roles.backup"),
      ofWhich: (name) => t("flow.ofWhich", { name }),
    }),
    power: props.get(`${id}/power`),
  })),
);
const fmt = (w: number | undefined) => formatPower(w, locale.value as Lang);
/** Battery and grid: magnitude plus "charging" / "import" etc. instead of a sign. */
const value = (w: number | undefined) => {
  const d = directed(props.node?.kind ?? "pv", w);
  const dir = d.dir === null ? "" : `${dirText.value[d.dir]} `;
  return { dir, power: fmt(d.power) };
};
const dirText = computed(() => ({
  charging: t("flow.charging"),
  discharging: t("flow.discharging"),
  import: t("flow.import"),
  export: t("flow.export"),
}));
</script>

<template>
  <!-- autofocus: opening by tap must not put a focus ring on the close button. -->
  <dialog
    ref="dialog"
    autofocus
    tabindex="-1"
    class="sheet glass m-auto w-[min(26rem,calc(100%-2rem))] rounded-card p-0 text-ink"
    @close="emit('close')"
    @click.self="dialog?.close()"
  >
    <div v-if="node" class="flex flex-col gap-3 p-5">
      <div class="flex items-center justify-between gap-3">
        <h2 class="text-base font-semibold">{{ label }}</h2>
        <button
          type="button"
          class="grid size-11 place-items-center rounded-control hover:bg-well"
          :aria-label="t('common.close')"
          @click="dialog?.close()"
        >
          <X :size="18" aria-hidden="true" />
        </button>
      </div>
      <dl class="flex flex-col">
        <div
          class="flex justify-between border-b border-line py-2 font-semibold"
        >
          <dt>{{ t("flow.total") }}</dt>
          <dd class="tabular-nums">
            <span class="font-normal text-ink-2">{{ value(node.power).dir }}</span>
            {{ value(node.power).power }}
          </dd>
        </div>
        <div v-for="r in rows" :key="r.id" class="flex justify-between py-2">
          <dt class="truncate text-ink-2">{{ r.label }}</dt>
          <dd class="shrink-0 tabular-nums">
            <span class="text-ink-2">{{ value(r.power).dir }}</span>
            {{ value(r.power).power }}
          </dd>
        </div>
      </dl>
    </div>
  </dialog>
</template>

<style scoped>
.sheet::backdrop {
  background: rgb(0 0 0 / 0.3);
}
</style>
