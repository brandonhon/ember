<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  let { onClose }: { onClose: () => void } = $props();

  const shortcuts: Array<[string, string]> = [
    ["j", "Next article"],
    ["k", "Previous article"],
    ["m", "Toggle read on selected article"],
    ["s", "Toggle star on selected article"],
    ["o", "Open original article in a new tab"],
    ["r", "Refresh sidebar + article list"],
    ["/", "Focus the search bar"],
    ["?", "Show this help"],
    ["Esc", "Close this help"],
  ];

  function onKey(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    }
  }
  onMount(() => window.addEventListener("keydown", onKey));
  onDestroy(() => window.removeEventListener("keydown", onKey));
</script>

<div
  class="backdrop"
  role="dialog"
  aria-modal="true"
  aria-labelledby="shortcut-help-title"
  on:click={onClose}
  data-testid="shortcut-help"
>
  <div class="card" on:click|stopPropagation>
    <header>
      <h2 id="shortcut-help-title">Keyboard shortcuts</h2>
      <button class="close" on:click={onClose} aria-label="Close">×</button>
    </header>
    <table>
      <tbody>
        {#each shortcuts as [key, label]}
          <tr>
            <td><kbd>{key}</kbd></td>
            <td>{label}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.45);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
  }
  .card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 1.25rem 1.5rem;
    min-width: 360px;
    color: var(--text);
  }
  header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 0.5rem;
  }
  h2 { margin: 0; font-family: "Fraunces", serif; }
  .close {
    background: transparent;
    border: 0;
    cursor: pointer;
    font-size: 1.5rem;
    color: var(--muted);
  }
  table { width: 100%; border-collapse: collapse; }
  td {
    padding: 0.35rem 0;
    border-bottom: 1px solid var(--border);
    font-size: 0.9rem;
  }
  td:first-child {
    width: 4rem;
    color: var(--muted);
  }
  kbd {
    display: inline-block;
    border: 1px solid var(--border);
    border-radius: 3px;
    padding: 0.05rem 0.4rem;
    font-family: ui-monospace, monospace;
    background: var(--badge-bg);
  }
</style>
