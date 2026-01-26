# End-User Personas

These personas represent the diverse user base of ContextHelp, ranging from non-technical users relying on the tool's privacy and intelligence, to power users integrating it into complex workflows.

## 1. The Overwhelmed PhD Candidate
**Name:** **Elena**
**Industry:** Academia / History
**Tech Savviness:** ⭐⭐ (Comfortable with citation software, intimidated by terminal)

*   **Context:** She has 4,000 PDFs, scanned letters, and handwritten notes on 18th-century trade routes. She is terrified of uploading her thesis research to a cloud AI that might hallucinate or train on her work.
*   **Objective:** Find connections between documents she read three years ago without re-reading everything.
*   **Use Case:** She uses a simple script her lab tech set up to "watch" her research folder. She relies heavily on the **OCR Pipeline** to make her scanned images searchable and asks her local Agent, "What did Governor Harrison say about silk tariffs?"

## 2. The Privacy-First Software Architect
**Name:** **Marcus**
**Industry:** Tech / FinTech
**Tech Savviness:** ⭐⭐⭐⭐⭐ (Lives in the terminal, compiles his own kernel)

*   **Context:** Works for a bank where data leakage is a fireable offense. He needs an AI coding assistant but cannot legally use Copilot or ChatGPT for internal code.
*   **Objective:** A coding assistant that understands the bank's legacy COBOL codebase and internal proprietary frameworks.
*   **Use Case:** He runs ContextHelp locally on his M3 Max. He ingests the company's git repos using the `url.repo` pipeline and configures a **Private Agent Profile** that only sees the `internal-docs` registry. He interacts via the **gRPC API** directly from his Neovim setup.

## 3. The Investigative Journalist
**Name:** **Sarah**
**Industry:** Journalism / Media
**Tech Savviness:** ⭐⭐⭐ (Uses encryption, Signal, and secure drops; willing to learn CLI for safety)

*   **Context:** She just received a "data dump" of 50,000 emails and financial documents related to a corruption scandal. She is offline in a hotel room to prevent remote hacks.
*   **Objective:** "Connect the dots" between shell companies mentioned in different PDF attachments.
*   **Use Case:** She uses ContextHelp's **Local-First Engine** to ingest the dump. she uses the **Semantic Hints** feature (`--hints "#money-laundering"`) to force the AI to prioritize financial relationships during indexing. She queries the data using natural language to find contradictions in dates.

## 4. The Sci-Fi Novelist
**Name:** **Julian**
**Industry:** Creative Writing
**Tech Savviness:** ⭐⭐ (Uses Scrivener and Obsidian)

*   **Context:** He is writing the 3rd book in a complex trilogy. He constantly forgets his own lore (e.g., "Did I say the warp drive uses blue or green crystals in Book 1?").
*   **Objective:** A "Lore Bible" that answers questions about his own universe instantly.
*   **Use Case:** He ingests his previous manuscripts (text files) and his wiki notes. He uses a **Custom Agent** named "LoreKeeper" configured with a weight registry that prioritizes "Canon" files over "Draft" files. He asks, "List all characters who have visited Planet X."

## 5. The Corporate Legal Associate
**Name:** **Priya**
**Industry:** Law
**Tech Savviness:** ⭐ (Needs a GUI or web interface)

*   **Context:** Dealing with "Discovery"—sifting through thousands of emails to find evidence of a contract breach.
*   **Objective:** Find every email sent by "John Doe" that mentions "Project Alpha" and "Delays" in Q3.
*   **Use Case:** She accesses ContextHelp via a **Web Dashboard** (connected to the local `ch serve` API). She relies on the **Taxonomy Registry** provided by her firm which automatically tags documents with legal concepts like "Force Majeure" or "Indemnity" so she doesn't have to keyword search for every synonym.

## 6. The Bio-Hacker / Self-Quantifier
**Name:** **Tom**
**Industry:** Health / Wellness
**Tech Savviness:** ⭐⭐⭐⭐ (Python scripter, automation enthusiast)

*   **Context:** Tracks his blood work, sleep data (Oura ring), supplements, and reads dense medical journals on longevity.
*   **Objective:** Determine if his supplement stack is actually improving his deep sleep based on the latest research.
*   **Use Case:** He ingests his CSV health logs and medical PDFs. He sets up an **Agent** with a strict scientific **Weights Registry** (prioritizing peer-reviewed papers over blog posts). He asks, "Based on my logs and the Huberman papers, is Magnesium Threonate working for me?"

## 7. The Investment Analyst
**Name:** **Victor**
**Industry:** Finance / Hedge Fund
**Tech Savviness:** ⭐⭐⭐ (Excel wizard, basic SQL/Python)

*   **Context:** Monitors 50 different biotech companies. Needs to react instantly to earnings calls and FDA reports.
*   **Objective:** Sentiment analysis. "Are the CEOs sounding more confident about Phase 3 trials than last quarter?"
*   **Use Case:** He feeds audio recordings of earnings calls into the **Audio Pipeline** (`audio.transcript`). He uses a **Comparison Agent** to query: "Compare the tone of the CEO's opening statement in Q1 vs Q3 regarding the FDA approval."

## 8. The Indie Game Developer
**Name:** **Alex**
**Industry:** Gaming
**Tech Savviness:** ⭐⭐⭐⭐⭐ (Unity/Unreal, C#)

*   **Context:** Wearing all hats: coding, marketing, art, and sound design. Has terabytes of reference images, texture packs, and spaghetti code.
*   **Objective:** Asset management and code recall. "Where is that script I wrote for the jumping mechanic two years ago?"
*   **Use Case:** He uses the **Image Pipeline** to index his texture library (AI tags images like "medieval," "stone," "tile"). He uses the CLI to quickly search code snippets: `ch list --tag "mechanic:jump" --type text`.

## 9. The Chief of Staff
**Name:** **Morgan**
**Industry:** Tech Start-up
**Tech Savviness:** ⭐⭐⭐ (Notion power user, Zapier automator)

*   **Context:** The "information hub" of the company. Attends every meeting, manages the CEO's brain, and organizes the internal wiki.
*   **Objective:** Answer team questions so the CEO doesn't have to. "What was the decision we made about the pricing tier in the October offsite?"
*   **Use Case:** Morgan records meetings (with consent) and ingests the transcripts. They use a **Team Registry** to ensure everyone uses the same vocabulary for project names. Morgan asks ContextHelp: "Draft a memo summarizing the action items for the Engineering team from the last 3 product syncs."

## 10. The Digital Rights Activist
**Name:** **Elias**
**Industry:** NGO / Non-Profit
**Tech Savviness:** ⭐⭐⭐⭐ (Decentralization advocate, Linux user)

*   **Context:** Works in a region with heavy internet censorship. Needs to curate and distribute educational materials on digital security that can be accessed offline.
*   **Objective:** Create a portable, censorship-resistant library of knowledge.
*   **Use Case:** Elias curates a **Public Taxonomy Registry** of anti-censorship tools. He distributes a "Starter Kit"—a USB drive containing the ContextHelp binary and a pre-indexed SQLite database of bookmarks—allowing users to run a smart, local search engine without connecting to the internet.
