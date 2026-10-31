<template>
  <button type="button" class="btn btn-sm lt-soft-action-btn">
    <slot />
  </button>
</template>

<style scoped>
.lt-soft-action-btn {
  --bs-btn-color: var(--lt-brand-ink);
  --bs-btn-bg: var(--lt-brand-soft);
  --bs-btn-border-color: transparent;
  --bs-btn-hover-color: var(--lt-brand-ink);
  --bs-btn-hover-bg: var(--lt-brand-soft-hover);
  --bs-btn-hover-border-color: transparent;
  --bs-btn-active-color: var(--lt-brand-ink);
  --bs-btn-active-bg: var(--lt-brand-soft);
  --bs-btn-active-border-color: transparent;
  font-weight: 500;
}

.lt-soft-action-btn:focus-visible {
  box-shadow: 0 0 0 0.16rem var(--lt-brand-focus-ring);
}
</style>
