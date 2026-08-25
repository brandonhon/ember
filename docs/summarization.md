# Summarization

Ember can generate a short AI summary for every article. By default it uses a local
Ollama model and nothing leaves your server; it can also talk to any OpenAI-compatible
endpoint or to Claude — see [Backends](#backends). This page covers how an article gets a
summary, the `summary_model` state that tracks where each article is in that process, and
how to recover a backlog that's stuck.

## How an article gets a summary

1. The poller ingests an article. It's inserted with `summary_model` `NULL` — pending.
2. The article is queued for the summarizer. A pending article is held back from every
   list and count (the "summary gate") until either the summarizer finishes it, or the
   **summary grace window** (`EMBER_SUMMARY_GRACE_SECONDS`, default 120s, runtime-tunable
   in **Settings → Language model → Article visibility**) lapses — whichever comes first.
   That window exists so a slow model delays an article rather than hiding it indefinitely.
3. The summarizer calls the backend. On success, the text is stored and `summary_model` is
   set to the model name. On failure — backend down, empty output, a persist error, or the
   request exceeding `EMBER_SUMMARY_TIMEOUT_SECONDS` — Ember writes `summary_model =
   'skipped'` rather than leaving the row pending, so the article still surfaces.

`summary_timeout_seconds` accepts 10–900, but on the **Claude** backend only 10–600 is
reachable: the Anthropic SDK's non-streaming timeout calculation returns a flat 10-minute
default for Ember's `MaxTokens` setting, and that applies whenever it lands before the
caller's context deadline — so a value of 601–900 is silently clamped to 600 on Claude.
Ollama and the OpenAI-compatible backend have no client-level timeout and honour the full
range.

## Backends

Which transport does the work is chosen in **Settings → Language model → Backend** and
persisted, so it survives a restart and applies to the next article without one. The
`EMBER_SUMMARY_*` env vars set the boot-time default; the admin's choice wins from then on.

| Backend | Talks to | Needs | Article text leaves the host? |
| --- | --- | --- | --- |
| **Ollama** (default) | Ollama's native `/api/generate` | `EMBER_OLLAMA_URL` + a pulled model | No |
| **OpenAI-compatible** | `/v1/chat/completions` on any compatible endpoint: OpenAI, OpenRouter, Groq, Mistral, Gemini's compatibility endpoint, vLLM, llama.cpp, LiteLLM | a base URL; an API key only if the server wants one | Yes, to that endpoint |
| **Claude** | Anthropic's Messages API | an API key | Yes, to Anthropic |

All three run the same prompt and the same parser, so summaries look the same whichever
you pick. What differs is what the backend can do besides summarize:

- **Model pull, delete, the installed-model list, the active-model picker, the host
  recommendation and the temperature / Top P / context-window tuning are Ollama-only.**
  None of them mean anything for a hosted provider — there is no local cache to manage —
  so those endpoints answer `503 not_ollama` and the UI hides the cards.
- The API key is stored write-only. The server reports whether a key exists
  (`api_key_set`), never the value, and an empty key on save means "keep the stored one".
  Use **Clear stored key** to erase it.

A backend that cannot answer is refused at save time (`openai` with no base URL,
`anthropic` with no API key, a base URL that isn't `http`/`https` → `400`). A backend
that is selected but not yet configured leaves summarization off: incoming articles are
stamped `disabled` — visible straight away, and reversible with **Requeue drained
articles** once the configuration is finished — rather than `skipped`, which is terminal.

## Choosing what gets summarized

Summarizing everything is the right default only for a feed you read end to end. Most
feeds are mixed: you skim most of the articles and read the rest, and which is which is
only clear once you've seen the title. **On-demand mode** waits for you to say so —
an article arrives readable, with no summary, and gets one the moment you star it, save it
for later, or pin it to a board. Those are the same three signals that already keep an
article past the retention window, so there is nothing new to learn.

Three controls set it, at three scopes:

- **Server-wide** — **Settings → Language model → Summaries → Summarize**: *Every article*
  or *When I mark it*. This is the default every feed falls back to.
- **Per feed** — the feed's **⋯** menu in the sidebar: *Summaries: default* (follow the
  server), *every article*, *when I mark one*, or *never*.
- **Per board** — the ✨ control on a board row: whether pinning an article there counts as
  "I mean to read this". A board you file things in rather than read from should turn it
  off.

### Resolution order

For one article, in order — the first rule that applies decides:

1. **Per-feed "never"** wins outright. If every subscriber of the feed has turned
   summaries off for it, the article is stamped `'excluded'` and nothing else is
   consulted. It is an opt-*out*, and no mode overrides it.
2. **The effective feed mode.** A feed's subscribers vote, and **any subscriber wanting
   every article wins**: if even one of them has chosen *every article* — explicitly, or
   by inheriting a server default of *every article* — the feed summarizes everything.
   This is not a tie-break, it's the only safe rule: the summary lives on the shared
   article row, so one reader choosing on-demand must not take away a summary another
   reader asked for.
3. **The server-wide mode**, for any subscriber with no opinion of their own.

An article that on-demand mode declines to queue is stamped `'deferred'`, which satisfies
the summary gate — so it is readable straight away rather than hidden waiting for a
summary that isn't coming.

### A worked example

A shared feed, three subscribers, server-wide mode *When I mark it*:

| Subscriber | Their setting | Wants every article? |
| --- | --- | --- |
| Ana | *Summaries: default* | No — inherits *When I mark it* |
| Ben | *Summaries: every article* | **Yes** |
| Cara | *Summaries: never* | No — opted out |

Ben's choice decides it: the feed summarizes every article as it arrives. Cara still sees
no summaries — her opt-out is per account and blanks the text for her — and Ana gets the
summaries for free, because they were generated anyway. Had Ben switched to *default* or
*when I mark one*, the feed would fall to on-demand and new articles would arrive
`'deferred'` until someone starred, saved, or pinned one.

Switching **to** *every article* backfills, at either scope: the articles already stamped
`'deferred'` are cleared and re-queued, so the choice acts on the backlog and not only on
what gets published next. A feed-level switch backfills that feed; the server-wide switch
backfills everything still deferred. Switching the other way — to *when I mark one* —
deliberately un-summarizes nothing and enqueues nothing; it only changes what happens to
the next batch.

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
| `'disabled'` | Finalized while summaries were switched off. Also cleared, alongside every other non-`'excluded'` state, by **Resummarize all**; the poller re-stamps `'disabled'` the next time it lands if summaries are still off. | Yes | No |
| `'deferred'` | On-demand mode: not queued until a reader stars, saves, or pins it. Cleared by any of those three, by switching the feed to *every article*, and by **Resummarize all**, which re-defers it on the next poller tick if the feed is still on-demand. | Yes | No |

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
or **Resummarize all** for the whole database) for those instead. Resummarize all clears
every non-`'excluded'` state, including `'deferred'` and `'disabled'`, not just `'skipped'`
and finished summaries — it converges harmlessly, since the poller and the disabled-switch
path both re-stamp those markers on their next pass, but it does mean a Resummarize-all run
also re-processes articles that were merely deferred or drained, not only failures.

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

Switching the **mode** back to *Every article* works the same way one marker over: the
articles stamped `'deferred'` are cleared and re-queued, and nothing else is touched. Both
switches act on the backlog they created, so neither looks inert until the next poll.

## What each control does

| Control | Scope | Where | Marker it produces |
| --- | --- | --- | --- |
| Global summaries switch | Server-wide | Settings → Language model | `'disabled'` on every pending article when turned off |
| Per-feed opt-out ("Summaries: never") | Per account, per feed | Feed's **⋯** menu in the sidebar | `'excluded'` once every subscriber has opted out |
| Summarize: every article / when I mark it | Server-wide | Settings → Language model → Summaries | `'deferred'` on articles nobody has asked for |
| Summaries: default / every article / when I mark one | Per account, per feed | Feed's **⋯** menu in the sidebar | `'deferred'`, unless another subscriber wants every article |
| Summarize what I pin here | Per account, per board | ✨ on the board row in the sidebar | None — it decides whether pinning *clears* `'deferred'` |
