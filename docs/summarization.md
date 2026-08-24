# Summarization

Ember can generate a short AI summary for every article, using a local Ollama model —
nothing leaves your server. This page covers how an article gets a summary, the
`summary_model` state that tracks where each article is in that process, and how to
recover a backlog that's stuck.

## How an article gets a summary

1. The poller ingests an article. It's inserted with `summary_model` `NULL` — pending.
2. The article is queued for the summarizer. A pending article is held back from every
   list and count (the "summary gate") until either the summarizer finishes it, or the
   **summary grace window** (`EMBER_SUMMARY_GRACE_SECONDS`, default 120s, runtime-tunable
   in **Settings → Language model → Article visibility**) lapses — whichever comes first.
   That window exists so a slow model delays an article rather than hiding it indefinitely.
3. The summarizer calls Ollama. On success, the text is stored and `summary_model` is set
   to the model name. On failure — backend down, empty output, a persist error, or the
   request exceeding `EMBER_SUMMARY_TIMEOUT_SECONDS` — Ember writes `summary_model =
   'skipped'` rather than leaving the row pending, so the article still surfaces.

## The `summary_model` state table

`summary_model` is the whole state machine. The `summary` text column holds output only —
it is **never** the state, and an empty summary is not by itself terminal. That distinction
is exactly what made issue [#198](https://github.com/brandonhon/ember/issues/198) hard to
diagnose from the database: the intuitive fix, blanking `summary`, does nothing, because
nothing reads `summary` to decide whether an article is done.

| `summary_model` | Meaning | Visible in lists? | Counted in `pending_summary`? |
| --- | --- | --- | --- |
| `NULL` or `''` | Pending — the summarizer hasn't finished with it. | Only after the grace window | Yes |
| `'<model name>'` | Summarized successfully; `summary` holds the text. | Yes | No |
| `'skipped'` | Attempted and failed (backend down, empty output, persist error, timeout). | Yes | No |
| `'excluded'` | Every subscriber of the feed opted out of summaries. | Yes | No |
| `'disabled'` | Finalized while summaries were switched off. | Yes | No |
| `'deferred'` | On-demand mode: not queued until a reader asks for it. Not yet available. | Yes | No |

Every non-empty value satisfies the summary gate — that's why every give-up path writes a
terminal marker instead of leaving the row `NULL`.

To see the real distribution in your database:

```sql
SELECT IFNULL(summary_model,'(pending)') AS state, COUNT(*)
FROM articles GROUP BY 1 ORDER BY 2 DESC;
```

## Runbook: "Summarizing N articles…" never goes down

This is issue #198. Some articles never got finalized — most commonly a restart landing
between an article's insert and the poller's stamp — so they sit `NULL` forever, pinned in
the pending count and hidden behind the summary gate.

**First move:** **Settings → Language model → Summarization queue → Drain queue** (admin
only). This stamps every pending article `'disabled'`, which makes it visible immediately
and drops it out of the pending count. No text is lost — a pending article has no summary
yet by definition — but those articles won't be summarized until you requeue them.

To send drained articles back through the summarizer, use **Requeue** in the same panel. It
clears only the `'disabled'` marker; articles that genuinely failed (`'skipped'`) or were
opted out per-feed (`'excluded'`) are left alone on purpose — use **Resummarize** (per-feed,
or **Resummarize all** for the whole database) for those instead.

If you don't have shell access to the admin UI, the same actions are:

```
POST /api/admin/summaries/drain    → {"drained": N}
POST /api/admin/summaries/requeue  → {"reset": N, "enqueued": M}
```

**Do not try `UPDATE articles SET summary = '' WHERE summary IS NULL`.** That was the
hand-written fix the #198 reporter tried first, and it does nothing — `summary` isn't the
state. The only column that matters is `summary_model`.

## Turning summaries off

The global switch (**Settings → Language model**) is a stored setting, not just an env
default — it takes effect immediately and survives a restart. Turning it off:

- Stops new articles from being queued for summarization.
- **Drains the existing pending queue** — every article still `NULL`/`''` is stamped
  `'disabled'` in the same request, so nothing stays stuck "summarizing" while the feature
  is off.

Turning it back on **requeues exactly those articles** — the ones stamped `'disabled'` —
without re-running anything that had already succeeded, been skipped, or been excluded.

## What each control does

| Control | Scope | Where | Marker it produces |
| --- | --- | --- | --- |
| Global summaries switch | Server-wide | Settings → Language model | `'disabled'` on every pending article when turned off |
| Per-feed opt-out ("Don't summarize") | Per account, per feed | Feed's **⋯** menu in the sidebar | `'excluded'` once every subscriber has opted out |
| On-demand mode | Server-wide | Not yet available | `'deferred'` (planned) |
