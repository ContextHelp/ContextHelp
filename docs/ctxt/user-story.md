# User Story: `ctxt` in Daily Use

Jad is mid-meeting and someone drops a critical link in chat: a competitor teardown.
He doesn’t want to lose it, classify it, or open a browser tab graveyard.

He runs:

- `ctxt add https://example.com/competitor-teardown`

`ctxt` instantly stores it locally, queues enrichment, and returns:

- “Saved. Processing in background…”

Ten minutes later, Jad is walking out. He remembers one line from the teardown:
“usage-based pricing killed retention”.

He searches without precision:

- `ctxt find "usage pricing retention"`

Results show the teardown plus two older notes from a past product discussion.
He opens the most relevant one:

- `ctxt open 3`

Inside, the page is already structured:
summary, key claims, extracted entities, and a few auto-generated questions.

He wants to turn this into something shippable for his team, right now:

- `ctxt make brief --topic "Pricing risk: usage-based retention" --from 3`

`ctxt` generates a clean internal brief, linked to sources and entities,
and asks one clarifying question:

- “Target audience: execs or product team?”

Jad answers:

- `ctxt reply "product team"`

The brief is generated, saved, and exportable:

- `ctxt export  --id brief:pricing-retention --format md`

Later that day, Jad switches to “Founder mode” to avoid irrelevant noise:

- `ctxt profile set founder`

Now `ctxt` starts surfacing decisions, risks, and bets more aggressively.

Before tomorrow’s leadership sync, `ctxt` preps him automatically:

- `ctxt agenda --tomorrow`

It returns:
- top active initiatives
- last known decisions
- unresolved questions
- key context pulled from the knowledge graph

No folders. No ceremony. No lost thinking.
Just capture → meaning → retrieval → output.
