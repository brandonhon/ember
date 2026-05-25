<script lang="ts">
  // ConfirmDialog is a simple paper-and-ink modal that asks a yes/no
  // question. Replaces window.confirm() so the look matches the rest of
  // the app and so destructive actions get the same visual weight.

  let {
    title = "Are you sure?",
    message,
    confirmLabel = "Confirm",
    cancelLabel = "Cancel",
    destructive = false,
    busy = false,
    onConfirm,
    onCancel,
  }: {
    title?: string;
    message: string;
    confirmLabel?: string;
    cancelLabel?: string;
    destructive?: boolean;
    busy?: boolean;
    onConfirm: () => void;
    onCancel: () => void;
  } = $props();

  function onKey(e: KeyboardEvent) {
    if (e.key === "Escape") onCancel();
    if (e.key === "Enter") onConfirm();
  }
</script>

<svelte:window on:keydown={onKey} />

<div
  class="backdrop"
  role="dialog"
  aria-modal="true"
  aria-labelledby="confirm-title"
  on:click={onCancel}
  data-testid="confirm-dialog"
>
  <div class="card" on:click|stopPropagation>
    <h3 id="confirm-title">{title}</h3>
    <p class="msg">{message}</p>
    <div class="actions">
      <button type="button" class="ghost" on:click={onCancel} data-testid="confirm-cancel">
        {cancelLabel}
      </button>
      <button
        type="button"
        class:destructive
        on:click={onConfirm}
        disabled={busy}
        data-testid="confirm-ok"
      >
        {busy ? "Working…" : confirmLabel}
      </button>
    </div>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(33, 29, 24, 0.5);
    display: grid;
    place-items: center;
    z-index: 200;
    padding: 24px;
  }
  .card {
    background: var(--card);
    border: 1px solid var(--line);
    border-radius: 14px;
    box-shadow: var(--shadow-pane);
    padding: 22px 24px 18px;
    width: min(420px, 100%);
    color: var(--ink);
  }
  h3 {
    font-family: var(--font-display);
    font-size: 19px;
    font-weight: 500;
    margin: 0 0 10px;
    color: var(--ink);
  }
  .msg {
    font-family: var(--font-ui);
    font-size: 13.5px;
    line-height: 1.5;
    color: var(--ink-soft);
    margin: 0 0 18px;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
  .actions button {
    background: var(--ember);
    color: #fff;
    border: 0;
    padding: 7px 14px;
    border-radius: 8px;
    font-family: var(--font-ui);
    font-size: 12.5px;
    font-weight: 600;
    cursor: pointer;
  }
  .actions button:hover:not(:disabled) { background: var(--ember-soft); }
  .actions button.ghost {
    background: transparent;
    color: var(--ink);
    border: 1px solid var(--line);
  }
  .actions button.ghost:hover { background: var(--line-soft); }
  .actions button.destructive {
    background: #b3261e;
  }
  .actions button.destructive:hover:not(:disabled) {
    background: #c93f30;
  }
  .actions button:disabled { opacity: 0.55; cursor: not-allowed; }
</style>
