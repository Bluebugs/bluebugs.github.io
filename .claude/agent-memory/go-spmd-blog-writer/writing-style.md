# Writing Style and Voice

## Tone
- **Conversational but technical**: Not academic, not casual. Reads like a senior engineer explaining to peers.
- **Opinionated with hedging**: "in my opinion", "I would think", "I believe" -- states positions but acknowledges subjectivity
- **Exploratory**: "What if", "Let's see", "How could we" -- framing as thought experiments
- **Honest about complexity**: Explicitly discusses trade-offs, doesn't oversell SPMD
- **Direct questions to reader**: "What do you think?", "Let me know if there is anything that need clarification"

## Voice Characteristics
- First person singular ("I believe", "I will explore") for opinions
- First person plural ("Let's look at", "we can", "we use") when walking through code
- Second person ("you", "your") when addressing the reader directly
- Occasional grammatical imperfections (non-native English speaker): "Let start with", "focus only on", "it keep its readability" -- DO NOT correct these in new posts; match this natural voice

## Paragraph Structure
- Short paragraphs, typically 2-4 sentences
- Code examples break up text frequently
- Bold keywords inline: **`go for`**, **`varying`**, **`uniform`**
- Backtick-wrapped inline code: `reduce.Any`, `lanes.Count`
- Key terms bolded on first introduction: **SPMD**, **SIMD**, **lane**, **mask**

## Section Headers
- H2 (##) for major sections
- H3 (###) for subsections
- Headers are conversational/question-form: "How Would _if_ Work?", "When Independent Lanes Aren't Enough"
- Italics sometimes used in headers: _if_, _for_
- No H1 headers in post body (title serves as H1)

## Explanation Pattern
The author follows a consistent pattern:
1. State the problem or question
2. Show Go code (or reference existing code)
3. Explain what the code does
4. Discuss implications or trade-offs
5. Connect to broader SPMD concepts

## Attribution Style
- Links to external research prominently: "inspired by [Name]'s [Title]"
- Credits original researchers by name: Wojciech Mula, Miguel Young de la Sota
- Links to GitHub issues for Go stdlib performance problems
- References ISPC documentation and Mojo documentation

## Closing Patterns
- Summary sections with bold bullet points
- "What do you think?" or invitation for feedback
- Series navigation links at the very bottom
- Horizontal rule before series navigation

## What NOT to Do
- No marketing language ("revolutionary", "game-changing")
- No excessive enthusiasm or superlatives
- No emoji in any post content
- No "click-bait" titles (titles are descriptive)
- Don't over-explain Go basics (audience is experienced Go devs)
