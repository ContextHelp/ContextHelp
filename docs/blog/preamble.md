# Preamble: Why I Am Building context.help

Are you feeling it too?

That low-grade frustration where LLMs are obviously useful, but only after you spend an unreasonable amount of time feeding them the context they need to not say something dumb.

I kept wondering whether everyone else had a smooth setup I had somehow missed. Maybe I was doing it wrong. Maybe there was a tool, a workflow, a magical combination of prompts and integrations.

So I tried.

## What I Tried Before Giving Up

I did the normal things first.

- Copy/paste the “right parts” into the chat
- Maintain a “prompt library”
- Keep running notes so I could quickly re-share decisions and constraints

Then I went deep on OSS, because surely someone had already built the missing pieces.

- Prompt repos, templates, and “skills” collections (great ideas, hard to keep current)
- Prompt runners and eval harnesses (useful, but they do not solve context assembly)
- Agent frameworks (AutoGPT-style loops, CrewAI-style orchestration, OpenHands-style tool use)
- RAG stacks and frameworks (LangChain, LlamaIndex, Haystack)
- UI builders around those stacks (Flowise, Dify)
- Local indexing/vector DB experiments ("just embed everything" sounds great until formats and provenance bite)
- DIY pipelines: `pandoc` to markdown, `rg`/`jq` for extraction, small scripts to chunk, tag, and re-pack context

All of it helped in pieces. None of it made the overall workflow feel stable and boring.

That works for a bit. Then the same failure loop kicks in:

- the context is in your world, not the model’s
- raw content is huge
- token windows shrink fast
- “just paste the doc” becomes impossible
- you start editing for tokens instead of thinking about the problem

So I tried to get smarter about it.

I started converting things into markdown.
Not because markdown is trendy, but because markup is pure overhead for a model. HTML, PDF structure, random export junk: tokens burned on presentation that adds no value.

Markdown was the first thing that reliably helped:

- less noise
- less token spend
- structure preserved (headings, lists, code)

But even with cleaner inputs, it still felt like I was doing the same job: manual retrieval, manual packing, manual context assembly.

Then I tried “solutions.”

Some were great at chatting with a pile of files.
Some were great at note-taking.
Some had decent retrieval.

But they kept missing something important:

- they did not treat knowledge as a PKMS (capture, retrieval, synthesis over time)
- they were not built for agents and tool-using workflows
- they required moving everything into one vendor’s ecosystem
- they did not support the formats I actually live in (PDFs, docs, code, exports, logs)
- they were not designed for decentralized storage or selective syndication

And then there was the vendor reality.

## The Vendor Problem

The ecosystem is moving at breakneck speed. Everyone ships constantly. Everyone copies everyone.

But there is still no shared protocol layer.

So the integration tax lands on you:

- a different configuration per vendor
- a different workflow per surface area
- a constant background task of keeping things aligned

What should be “swap the engine” becomes “rebuild the car.”

## The Token Incentive Problem

Meanwhile, token billing quietly shapes everything.

If tokens are the meter, then “processing more tokens” correlates with revenue.
Users want the opposite:

- smaller prompts
- fewer retries
- less verbosity
- more grounding
- faster time to correct

No one is strongly incentivized to drive your token usage down.
So if you care about efficiency, you end up having to build it into your own workflow.

At some point I realized I was not missing a trick.
I was bumping into a missing layer.

## The Birth of context.help

I did not set out to build a product.
I set out to stop bleeding time on context.

context.help is my attempt at that missing layer: something that makes context cheap to ingest, portable across vendors, and useful over time.

The bar I am trying to hit is simple:

- Feeding models should not require manual copy/paste curation.
- Context should be stored in a format that minimizes noise (markdown is the first step).
- You should be able to switch vendors without rewriting your workflow.
- The system should work like a real PKMS, not a graveyard of clips.
- Agents should be able to consume it, verify against it, and improve their work.

If that sounds familiar, you are the audience.

Everything else is implementation detail.
