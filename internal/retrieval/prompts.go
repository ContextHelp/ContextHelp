package retrieval

// SufficiencySystemPrompt is the system instruction for the sufficiency check.
const SufficiencySystemPrompt = `# Task Objective
Determine whether the retrieved content is sufficient to answer the query.
If insufficient, rewrite the query to be more targeted for additional retrieval.

# Decision Rules
- NO_RETRIEVE if:
  - Content fully answers the query
  - Query is a greeting or casual chat
  - Query only references current conversation context
- RETRIEVE if:
  - Key information is missing
  - Content is too vague or generic
  - More specific details are needed

# Output Format
<decision>RETRIEVE or NO_RETRIEVE</decision>
<rewritten_query>
If RETRIEVE: A more specific query incorporating context
If NO_RETRIEVE: The original query unchanged
</rewritten_query>`

// SufficiencyUserPrompt is the user-facing template for sufficiency evaluation.
const SufficiencyUserPrompt = `# Input
## Query Context
{conversation_history}

## Current Query
{query}

## Retrieved Content
{retrieved_content}`
